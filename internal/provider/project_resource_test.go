package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ConfigDirector/terraform-provider-configdirector/internal/client"
)

func TestAccProjectResource_createAndImport(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("configdirector_project.test", "id"),
					resource.TestCheckResourceAttr("configdirector_project.test", "name", p.Name),
					resource.TestCheckResourceAttr("configdirector_project.test", "slug", p.Slug),
					resource.TestCheckResourceAttrSet("configdirector_project.test", "organization_id"),
					resource.TestCheckResourceAttr("configdirector_project.test", "environments.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs("configdirector_project.test", "environments.*", map[string]string{
						"slug": "test",
						"live": "false",
					}),
					resource.TestCheckTypeSetElemNestedAttrs("configdirector_project.test", "environments.*", map[string]string{
						"slug": "production",
						"live": "true",
					}),
				),
			},
			{
				ResourceName:      "configdirector_project.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccProjectResource_importBySlug covers ImportState's alternate lookup
// path: the project's slug, not just its id (see project_resource.go's
// ImportState, which falls back to a ListProjects scan for slugs).
func TestAccProjectResource_importBySlug(t *testing.T) {
	p := newTestAccProject(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test"),
			},
			{
				ResourceName:      "configdirector_project.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["configdirector_project.test"]
					if !ok {
						return "", nil
					}
					return rs.Primary.Attributes["slug"], nil
				},
			},
		},
	})
}

func TestAccProjectResource_renameUpdatesInPlace(t *testing.T) {
	p := newTestAccProject(t)
	renamed := testAccProject{Name: p.Name + " Renamed", Slug: p.Slug}
	var idBeforeUpdate string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test"),
				Check: resource.TestCheckResourceAttrWith("configdirector_project.test", "id", func(value string) error {
					idBeforeUpdate = value
					return nil
				}),
			},
			{
				Config: renamed.config("test"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_project.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_project.test", "name", renamed.Name),
					resource.TestCheckResourceAttrWith("configdirector_project.test", "id", func(value string) error {
						if value != idBeforeUpdate {
							return fmt.Errorf("expected id to remain %q after rename, got %q", idBeforeUpdate, value)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccProjectResource_slugChangeUpdatesInPlace(t *testing.T) {
	p := newTestAccProject(t)
	reslugged := testAccProject{Name: p.Name, Slug: p.Slug + "-renamed"}
	var idBeforeUpdate string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test"),
				Check: resource.TestCheckResourceAttrWith("configdirector_project.test", "id", func(value string) error {
					idBeforeUpdate = value
					return nil
				}),
			},
			{
				Config: reslugged.config("test"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_project.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("configdirector_project.test", "slug", reslugged.Slug),
					resource.TestCheckResourceAttrWith("configdirector_project.test", "id", func(value string) error {
						if value != idBeforeUpdate {
							return fmt.Errorf("expected id to remain %q after slug change, got %q", idBeforeUpdate, value)
						}
						return nil
					}),
				),
			},
		},
	})
}

// TestAccProjectResource_deletedOutOfBand verifies Read()'s 404 handling:
// when a project has been deleted outside of Terraform, refreshing state
// should drop it from state (rather than erroring), leaving a plan that
// wants to create it again.
func TestAccProjectResource_deletedOutOfBand(t *testing.T) {
	p := newTestAccProject(t)
	var projectID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: p.config("test"),
				Check: resource.TestCheckResourceAttrWith("configdirector_project.test", "id", func(value string) error {
					projectID = value
					return nil
				}),
			},
			{
				RefreshState: true,
				PreConfig: func() {
					c := client.New(testAccBaseURL(), os.Getenv("CONFIGDIRECTOR_TOKEN"))
					if err := c.DeleteProject(context.Background(), projectID); err != nil {
						t.Fatalf("deleting project out of band: %s", err)
					}
				},
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("configdirector_project.test", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// TestAccProjectResource_validation covers the schema-level validators on
// name/slug. These fail during plan, before any API call is made.
func TestAccProjectResource_validation(t *testing.T) {
	p := newTestAccProject(t)

	testCases := map[string]struct {
		config      string
		expectError string
	}{
		"name too long": {
			config:      testAccProject{Name: strings.Repeat("a", 256), Slug: p.Slug}.config("test"),
			expectError: `string length must be between 1 and 255`,
		},
		"slug with invalid characters": {
			config:      testAccProject{Name: p.Name, Slug: "Not A Valid Slug!"}.config("test"),
			expectError: `value must match regular expression`,
		},
		"slug with consecutive separators": {
			config:      testAccProject{Name: p.Name, Slug: "test--project"}.config("test"),
			expectError: `value must match regular expression`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      tc.config,
						ExpectError: regexp.MustCompile(tc.expectError),
					},
				},
			})
		})
	}
}
