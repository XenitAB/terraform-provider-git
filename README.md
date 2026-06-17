# Terraform Provider Git

## Testing locally

Before creating a release and uploading the plugin to the registry, test the plugin locally. To do this, you need to [create a CLI configuration file](https://developer.hashicorp.com/terraform/cli/config/config-file#provider_installation), unless you already have done so. On Linux, the file should be named `.terraformrc` and be placed in the user directory. The content of it should resemble the file below:

  

    provider_installation {
      filesystem_mirror {
        path    = "/home/my-user/dev/terraform-providers"
        include = ["registry.terraform.io/xenitab/git"]
      }
      direct {
        include = ["registry.terraform.io/*/*"]
      }
    }
    
Then, assuming you want to test version X.Y.Z of the plugin, you should create the following directory tree directly beneath the `terraform-providers` directory and place the `terraform-provider-git` binary in the `linux_amd64` directory.

  

    - registry.terraform.io
      |- xenitab
         |- git
           |- X.Y.Z
              |- linux_amd64

Now, when you use the CLI, the provider will be downloaded by the CLI from this location.

When you are done testing, don't forget to remove this file from your hoe directory, or at least disable the `filesystem_mirror` block.

**Note:** If you are using OpenTofu, use the **registry.opentofu.org** `include` and `directory` name instead