// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// ResourceBuilder is a helper struct to build resources predefined in metadata.yaml.
// The ResourceBuilder is not thread-safe and must not to be used in multiple goroutines.
type ResourceBuilder struct {
	config ResourceAttributesConfig
	res    pcommon.Resource
}

// NewResourceBuilder creates a new ResourceBuilder. This method should be called on the start of the application.
func NewResourceBuilder(rac ResourceAttributesConfig) *ResourceBuilder {
	res := pcommon.NewResource()

	return &ResourceBuilder{
		config: rac,
		res:    res,
	}
}

// SetAzureResourcegroupName sets provided value as "azure.resourcegroup.name" attribute.
func (rb *ResourceBuilder) SetAzureResourcegroupName(val string) {
	if rb.config.AzureResourcegroupName.Enabled {
		rb.res.Attributes().PutStr("azure.resourcegroup.name", val)
	}
}

// SetAzureVMName sets provided value as "azure.vm.name" attribute.
func (rb *ResourceBuilder) SetAzureVMName(val string) {
	if rb.config.AzureVMName.Enabled {
		rb.res.Attributes().PutStr("azure.vm.name", val)
	}
}

// SetAzureVMScalesetName sets provided value as "azure.vm.scaleset.name" attribute.
func (rb *ResourceBuilder) SetAzureVMScalesetName(val string) {
	if rb.config.AzureVMScalesetName.Enabled {
		rb.res.Attributes().PutStr("azure.vm.scaleset.name", val)
	}
}

// SetAzureVMSize sets provided value as "azure.vm.size" attribute.
func (rb *ResourceBuilder) SetAzureVMSize(val string) {
	if rb.config.AzureVMSize.Enabled {
		rb.res.Attributes().PutStr("azure.vm.size", val)
	}
}

// SetCloudAccountID sets provided value as "cloud.account.id" attribute.
func (rb *ResourceBuilder) SetCloudAccountID(val string) {
	if rb.config.CloudAccountID.Enabled {
		rb.res.Attributes().PutStr("cloud.account.id", val)
	}
}

// SetCloudPlatform sets provided value as "cloud.platform" attribute.
func (rb *ResourceBuilder) SetCloudPlatform(val string) {
	if rb.config.CloudPlatform.Enabled {
		rb.res.Attributes().PutStr("cloud.platform", val)
	}
}

// SetCloudProvider sets provided value as "cloud.provider" attribute.
func (rb *ResourceBuilder) SetCloudProvider(val string) {
	if rb.config.CloudProvider.Enabled {
		rb.res.Attributes().PutStr("cloud.provider", val)
	}
}

// SetCloudRegion sets provided value as "cloud.region" attribute.
func (rb *ResourceBuilder) SetCloudRegion(val string) {
	if rb.config.CloudRegion.Enabled {
		rb.res.Attributes().PutStr("cloud.region", val)
	}
}

// SetHostID sets provided value as "host.id" attribute.
func (rb *ResourceBuilder) SetHostID(val string) {
	if rb.config.HostID.Enabled {
		rb.res.Attributes().PutStr("host.id", val)
	}
}

// SetHostName sets provided value as "host.name" attribute.
func (rb *ResourceBuilder) SetHostName(val string) {
	if rb.config.HostName.Enabled {
		rb.res.Attributes().PutStr("host.name", val)
	}
}

// Emit returns the built resource and resets the internal builder state.
func (rb *ResourceBuilder) Emit() pcommon.Resource {
	r := rb.res
	rb.res = pcommon.NewResource()
	return r
}
