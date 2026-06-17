resource "git_repository_file" "this" {
  path    = "README.md"
  content = "Hello World"
}

# Write the file to a unique, short-lived branch created per run. Referencing
# computed_name creates an implicit dependency, so the branch is guaranteed to
# exist before the file is written.
resource "git_repository_branch" "run" {
  name             = "deploy"
  append_timestamp = true
}

resource "git_repository_file" "on_branch" {
  path    = "STATUS.md"
  content = "Hello World"
  branch  = git_repository_branch.run.computed_name
}
