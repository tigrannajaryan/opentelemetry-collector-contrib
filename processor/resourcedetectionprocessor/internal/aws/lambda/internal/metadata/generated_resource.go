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

// SetAwsLogGroupNames sets provided value as "aws.log.group.names" attribute.
func (rb *ResourceBuilder) SetAwsLogGroupNames(val []any) {
	if rb.config.AwsLogGroupNames.Enabled {
		rb.res.Attributes().PutEmptySlice("aws.log.group.names").FromRaw(val)
	}
}

// SetAwsLogStreamNames sets provided value as "aws.log.stream.names" attribute.
func (rb *ResourceBuilder) SetAwsLogStreamNames(val []any) {
	if rb.config.AwsLogStreamNames.Enabled {
		rb.res.Attributes().PutEmptySlice("aws.log.stream.names").FromRaw(val)
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

// SetFaasInstance sets provided value as "faas.instance" attribute.
func (rb *ResourceBuilder) SetFaasInstance(val string) {
	if rb.config.FaasInstance.Enabled {
		rb.res.Attributes().PutStr("faas.instance", val)
	}
}

// SetFaasMaxMemory sets provided value as "faas.max_memory" attribute.
func (rb *ResourceBuilder) SetFaasMaxMemory(val string) {
	if rb.config.FaasMaxMemory.Enabled {
		rb.res.Attributes().PutStr("faas.max_memory", val)
	}
}

// SetFaasName sets provided value as "faas.name" attribute.
func (rb *ResourceBuilder) SetFaasName(val string) {
	if rb.config.FaasName.Enabled {
		rb.res.Attributes().PutStr("faas.name", val)
	}
}

// SetFaasVersion sets provided value as "faas.version" attribute.
func (rb *ResourceBuilder) SetFaasVersion(val string) {
	if rb.config.FaasVersion.Enabled {
		rb.res.Attributes().PutStr("faas.version", val)
	}
}

// Emit returns the built resource and resets the internal builder state.
func (rb *ResourceBuilder) Emit() pcommon.Resource {
	r := rb.res
	rb.res = pcommon.NewResource()
	return r
}
