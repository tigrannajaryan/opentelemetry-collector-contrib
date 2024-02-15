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

// SetElasticsearchClusterName sets provided value as "elasticsearch.cluster.name" attribute.
func (rb *ResourceBuilder) SetElasticsearchClusterName(val string) {
	if rb.config.ElasticsearchClusterName.Enabled {
		rb.res.Attributes().PutStr("elasticsearch.cluster.name", val)
	}
}

// SetElasticsearchIndexName sets provided value as "elasticsearch.index.name" attribute.
func (rb *ResourceBuilder) SetElasticsearchIndexName(val string) {
	if rb.config.ElasticsearchIndexName.Enabled {
		rb.res.Attributes().PutStr("elasticsearch.index.name", val)
	}
}

// SetElasticsearchNodeName sets provided value as "elasticsearch.node.name" attribute.
func (rb *ResourceBuilder) SetElasticsearchNodeName(val string) {
	if rb.config.ElasticsearchNodeName.Enabled {
		rb.res.Attributes().PutStr("elasticsearch.node.name", val)
	}
}

// SetElasticsearchNodeVersion sets provided value as "elasticsearch.node.version" attribute.
func (rb *ResourceBuilder) SetElasticsearchNodeVersion(val string) {
	if rb.config.ElasticsearchNodeVersion.Enabled {
		rb.res.Attributes().PutStr("elasticsearch.node.version", val)
	}
}

// Emit returns the built resource and resets the internal builder state.
func (rb *ResourceBuilder) Emit() pcommon.Resource {
	r := rb.res
	rb.res = pcommon.NewResource()
	return r
}
