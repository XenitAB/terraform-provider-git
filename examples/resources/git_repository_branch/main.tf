# Basic usage: create a branch named "feature" from the provider-configured default branch.
resource "git_repository_branch" "basic" {
  name = "feature"
}

# Create a branch from a specific source branch.
resource "git_repository_branch" "from_source" {
  name          = "hotfix"
  source_branch = "main"
}

# Append a timestamp suffix in YYYYMMDDHHMMSS (24-hour) format.
# The computed_name will be something like "release-20260610145259".
resource "git_repository_branch" "with_timestamp" {
  name             = "release"
  append_timestamp = true
}

# Use a custom Go time layout for the timestamp suffix.
# The computed_name will be something like "deploy-2026-06-10".
resource "git_repository_branch" "custom_format" {
  name             = "deploy"
  append_timestamp = true
  timestamp_format = "2006-01-02"
}
