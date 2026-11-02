package provider

const resourceDeletionProtectionDescription = "Whether deletion protection is on. It is set in the ConfigDirector dashboard; while on, destroying this resource fails. " +
	"Pair it with `lifecycle { prevent_destroy = true }` to keep Terraform from planning the destroy."

const projectDeletionProtectionDescription = "Whether deletion protection is on for the project. It is set in the ConfigDirector dashboard; while on, deleting the project fails."

const environmentDeletionProtectionDescription = "Whether deletion protection is on for the environment. It is set in the ConfigDirector dashboard; while on, deleting the environment or its project fails."
