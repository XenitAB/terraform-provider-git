terraform {
  required_version = ">=1.1.5"

  required_providers {
    github = {
      source  = "integrations/github"
      version = ">=5.20.0"
    }
    git = {
      source  = "registry.terraform.io/xenitab/git"
      version = ">=0.0.1"
    }
  }
}

resource "tls_private_key" "this" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

provider "github" {
  owner = var.github_org
  token = var.github_token
}

resource "github_repository" "this" {
  name             = "provider-git-test"
  visibility       = "private"
  license_template = "mit"
}

resource "github_repository_deploy_key" "this" {
  title      = "Flux"
  repository = github_repository.this.name
  key        = tls_private_key.this.public_key_openssh
  read_only  = "false"
}

provider "git" {
  url    = "ssh://git@github.com/${var.github_org}/provider-git-test.git"
  branch = "provider-git-test"
  ssh = {
    username    = "git"
    private_key = tls_private_key.this.private_key_pem
  }
}

# Unique, short-lived branch per run. computed_name is persisted in state and is
# stable across plan/apply/refresh within a run.
resource "git_repository_branch" "run" {
  depends_on = [github_repository_deploy_key.this]

  name             = "provider-git-test"
  append_timestamp = true
}

resource "git_repository_file" "this" {
  path    = "README.mddd"
  content = "Hello World 123"
  branch  = git_repository_branch.run.computed_name
}
