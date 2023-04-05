resource "git_repository_file" "this" {
  path    = "README.md"
  content = "Hello World"
}
