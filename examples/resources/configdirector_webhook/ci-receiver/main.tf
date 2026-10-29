variable "signing_secret" {
  type      = string
  sensitive = true
}

resource "terraform_data" "receiver_environment" {
  input = {
    CONFIGDIRECTOR_WEBHOOK_SECRET = var.signing_secret
  }
}
