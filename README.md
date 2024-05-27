# OpenTofu Provider

This is a [Krateo](https://krateoplatformops.github.io/) Provider that manages cloud infrastructure through OpenTofu.

## Notes
The OpenTofu provider leverages the [OpenTofu CLI](https://opentofu.org/docs/intro/install/) to manage cloud infrastructures.

See the following samples for a ready-to-use example with the AWS provider:
- [samples/tfconfig.yaml](https://github.com/krateoplatformops/opentofu-provider/blob/f80ed076bf73a7f0fc253518fce62071890fd3b2/samples/tfconfig.yaml)
- [samples/workspace.yaml](https://github.com/krateoplatformops/opentofu-provider/blob/f80ed076bf73a7f0fc253518fce62071890fd3b2/samples/workspace.yaml)
- [this repo](https://github.com/matteogastaldello/opentofu-example/tree/remote?ref=remote)

Provider credentials (e.g., AWS, GCP) are managed by the controllers via `tfconfig.spec.providerCredentials`. Ensure that the filename specified in `tfconfig.spec.providerCredentials.credFilename` is also set in the provider section of the "main.tf" file.
