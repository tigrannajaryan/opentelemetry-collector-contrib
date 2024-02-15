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

// SetRabbitmqNodeName sets provided value as "rabbitmq.node.name" attribute.
func (rb *ResourceBuilder) SetRabbitmqNodeName(val string) {
	if rb.config.RabbitmqNodeName.Enabled {
		rb.res.Attributes().PutStr("rabbitmq.node.name", val)
	}
}

// SetRabbitmqQueueName sets provided value as "rabbitmq.queue.name" attribute.
func (rb *ResourceBuilder) SetRabbitmqQueueName(val string) {
	if rb.config.RabbitmqQueueName.Enabled {
		rb.res.Attributes().PutStr("rabbitmq.queue.name", val)
	}
}

// SetRabbitmqVhostName sets provided value as "rabbitmq.vhost.name" attribute.
func (rb *ResourceBuilder) SetRabbitmqVhostName(val string) {
	if rb.config.RabbitmqVhostName.Enabled {
		rb.res.Attributes().PutStr("rabbitmq.vhost.name", val)
	}
}

// Emit returns the built resource and resets the internal builder state.
func (rb *ResourceBuilder) Emit() pcommon.Resource {
	r := rb.res
	rb.res = pcommon.NewResource()
	return r
}
