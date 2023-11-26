# OpenTofu Provider

This is a [Krateo](https://krateoplatformops.github.io/) Provider that clones git repositories (eventually applying templates).

## Notes
OpenTofu provider needs [OpenTofu CLI](https://opentofu.org/docs/intro/install/) to work. Tested with OpenTofu v1.6.0-dev

The provider can works with cloud backend (eg. Terraform Cloud). In that case `tfconfig.spec.backendCredentials` need to be set or operation like `terraform login` must be performed before applying the workspace resource. `workspace.spec.workspace.cloud` must be setted to `true`.

Provider (eg. AWS, GCP) credentials are managed by the controllers by `tfconfig.spec.providerCredentials`. The same filename setted in `tfconfig.spec.providerCredentials.credFilename` must be setted even in provider section on "main.tf" file.