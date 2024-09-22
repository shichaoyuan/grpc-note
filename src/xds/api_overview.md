# API概述

https://www.envoyproxy.io/docs/envoy/latest/configuration/overview/overview

## 引言

Envoy xDS APIs 以 proto3 格式定义，代码仓库位于[envoy/api](https://github.com/envoyproxy/envoy/tree/main/api)

## 版本

API语义版本基本上遵守[https://cloud.google.com/apis/design/versioning](https://cloud.google.com/apis/design/versioning)

Envoy APIs包含一系列包，包名与目录结构保持一致，每个包独立进行版本控制。

例如`envoy.admin.v3`：
```shell
 envoy
├──  admin
│   ├──  v2alpha
│   │   ├──  BUILD
│   │   ├──  certs.proto
│   │   ├──  clusters.proto
│   │   ├──  config_dump.proto
│   │   ├──  listeners.proto
│   │   ├──  memory.proto
│   │   ├──  metrics.proto
│   │   ├──  mutex_stats.proto
│   │   ├──  server_info.proto
│   │   └──  tap.proto
│   └──  v3
│       ├──  BUILD
│       ├──  certs.proto
│       ├──  clusters.proto
│       ├──  config_dump.proto
│       ├──  config_dump_shared.proto
│       ├──  init_dump.proto
│       ├──  listeners.proto
│       ├──  memory.proto
│       ├──  metrics.proto
│       ├──  mutex_stats.proto
│       ├──  server_info.proto
│       └──  tap.proto
```

### 兼容性
版本中不允许breaking change，需要遵守的规则：
* 字段不允许重新编号，也不允许改变类型
* 字段和包也不允许改名
    * 字段改名了会影响JSON/YAML格式
    * 包改名了会影响gRPC端点URL，也就是会影响客户端和服务端的通信
    * 嵌入在`Any`对象中的消息
* 还有一些对于pb是兼容的，但是破坏Envoy API的场景
    * 将单个字段改为repeated
    * 用`oneof`包装存在的字段
    * 增加protoc-gen-validate注解的严格性

但是修改包装类型（例如`google.protobuf.UInt32Value`）的默认值不受限制，管理服务应该显式设置默认值。

### API生命周期

引入不兼容的改动，升级主版本这一规则主要是为了解决技术债。但是考虑到Envoy和xDS生态的广泛性，升级版本实际上不现实，所以v3就是事实上的最终版本。

Deprecations还是会发生的，但是不会产生不兼容的影响。


## Bootstrap配置

Bootstrap配置提供最基本的配置，静态资源和动态资源。

`envoy/config/bootstrap/v3/bootstrap.proto`


```proto3
message Bootstrap {
  // 静态资源
  message StaticResources {
    // Listener
    repeated listener.v3.Listener listeners = 1;
    // Cluster
    repeated cluster.v3.Cluster clusters = 2;
    // 证书
    repeated envoy.extensions.transport_sockets.tls.v3.Secret secrets = 3;
  }

  // 动态资源
  message DynamicResources {
    reserved 4;

    // LDS 配置
    core.v3.ConfigSource lds_config = 1;
    // xdstp:// resource locator for listener collection.
    string lds_resources_locator = 5;

    // CDS 配置
    core.v3.ConfigSource cds_config = 2;
    // xdstp:// resource locator for cluster collection.
    string cds_resources_locator = 6;

    // ADS 配置
    core.v3.ApiConfigSource ads_config = 3;
  }

  reserved 10, 11;

  reserved "runtime";

  // Node identity to present to the management server and for instance
  // identification purposes (e.g. in generated headers).
  core.v3.Node node = 1;

  repeated string node_context_params = 26;

  // Statically specified resources.
  StaticResources static_resources = 2;
  // xDS configuration sources.
  DynamicResources dynamic_resources = 3;

  // Configuration for the cluster manager which owns all upstream clusters
  // within the server.
  ClusterManager cluster_manager = 4;

  // HDS 配置
  core.v3.ApiConfigSource hds_config = 14;

  // Optional file system path to search for startup flag files.
  string flags_path = 5;

  // Optional set of stats sinks.
  repeated metrics.v3.StatsSink stats_sinks = 6;

  // Options to control behaviors of deferred creation compatible stats.
  DeferredStatOptions deferred_stat_options = 39;

  // Configuration for internal processing of stats.
  metrics.v3.StatsConfig stats_config = 13;

  // Optional duration between flushes to configured stats sinks. For
  // performance reasons Envoy latches counters and only flushes counters and
  // gauges at a periodic interval. If not specified the default is 5000ms (5
  // seconds). Only one of ``stats_flush_interval`` or ``stats_flush_on_admin``
  // can be set.
  // Duration must be at least 1ms and at most 5 min.
  google.protobuf.Duration stats_flush_interval = 7 [
    (validate.rules).duration = {
      lt {seconds: 300}
      gte {nanos: 1000000}
    },
    (udpa.annotations.field_migrate).oneof_promotion = "stats_flush"
  ];

  oneof stats_flush {
    // Flush stats to sinks only when queried for on the admin interface. If set,
    // a flush timer is not created. Only one of ``stats_flush_on_admin`` or
    // ``stats_flush_interval`` can be set.
    bool stats_flush_on_admin = 29 [(validate.rules).bool = {const: true}];
  }

  // Optional watchdogs configuration.
  // This is used for specifying different watchdogs for the different subsystems.
  // [#extension-category: envoy.guarddog_actions]
  Watchdogs watchdogs = 27;

  // Configuration for the runtime configuration provider. If not
  // specified, a “null” provider will be used which will result in all defaults
  // being used.
  LayeredRuntime layered_runtime = 17;

  // Configuration for the local administration HTTP server.
  Admin admin = 12;

  // Optional overload manager configuration.
  overload.v3.OverloadManager overload_manager = 15 [
    (udpa.annotations.security).configure_for_untrusted_downstream = true,
    (udpa.annotations.security).configure_for_untrusted_upstream = true
  ];

  // Enable :ref:`stats for event dispatcher <operations_performance>`, defaults to false.
  // Note that this records a value for each iteration of the event loop on every thread. This
  // should normally be minimal overhead, but when using
  // :ref:`statsd <envoy_v3_api_msg_config.metrics.v3.StatsdSink>`, it will send each observed value
  // over the wire individually because the statsd protocol doesn't have any way to represent a
  // histogram summary. Be aware that this can be a very large volume of data.
  bool enable_dispatcher_stats = 16;

  // Optional string which will be used in lieu of x-envoy in prefixing headers.
  //
  // For example, if this string is present and set to X-Foo, then x-envoy-retry-on will be
  // transformed into x-foo-retry-on etc.
  //
  // Note this applies to the headers Envoy will generate, the headers Envoy will sanitize, and the
  // headers Envoy will trust for core code and core extensions only. Be VERY careful making
  // changes to this string, especially in multi-layer Envoy deployments or deployments using
  // extensions which are not upstream.
  string header_prefix = 18;

  // Optional proxy version which will be used to set the value of :ref:`server.version statistic
  // <server_statistics>` if specified. Envoy will not process this value, it will be sent as is to
  // :ref:`stats sinks <envoy_v3_api_msg_config.metrics.v3.StatsSink>`.
  google.protobuf.UInt64Value stats_server_version_override = 19;


  // DNS resolver type configuration extension. This extension can be used to configure c-ares, apple,
  // or any other DNS resolver types and the related parameters.
  // For example, an object of
  // :ref:`CaresDnsResolverConfig <envoy_v3_api_msg_extensions.network.dns_resolver.cares.v3.CaresDnsResolverConfig>`
  // can be packed into this ``typed_dns_resolver_config``. This configuration replaces the
  // :ref:`dns_resolution_config <envoy_v3_api_field_config.bootstrap.v3.Bootstrap.dns_resolution_config>`
  // configuration.
  // During the transition period when both ``dns_resolution_config`` and ``typed_dns_resolver_config`` exists,
  // when ``typed_dns_resolver_config`` is in place, Envoy will use it and ignore ``dns_resolution_config``.
  // When ``typed_dns_resolver_config`` is missing, the default behavior is in place.
  // [#extension-category: envoy.network.dns_resolver]
  core.v3.TypedExtensionConfig typed_dns_resolver_config = 31;

  // Specifies optional bootstrap extensions to be instantiated at startup time.
  // Each item contains extension specific configuration.
  // [#extension-category: envoy.bootstrap]
  repeated core.v3.TypedExtensionConfig bootstrap_extensions = 21;

  // Specifies optional extensions instantiated at startup time and
  // invoked during crash time on the request that caused the crash.
  repeated FatalAction fatal_actions = 28;

  // Configuration sources that will participate in
  // xdstp:// URL authority resolution. The algorithm is as
  // follows:
  // 1. The authority field is taken from the xdstp:// URL, call
  //    this ``resource_authority``.
  // 2. ``resource_authority`` is compared against the authorities in any peer
  //    ``ConfigSource``. The peer ``ConfigSource`` is the configuration source
  //    message which would have been used unconditionally for resolution
  //    with opaque resource names. If there is a match with an authority, the
  //    peer ``ConfigSource`` message is used.
  // 3. ``resource_authority`` is compared sequentially with the authorities in
  //    each configuration source in ``config_sources``. The first ``ConfigSource``
  //    to match wins.
  // 4. As a fallback, if no configuration source matches, then
  //    ``default_config_source`` is used.
  // 5. If ``default_config_source`` is not specified, resolution fails.
  // [#not-implemented-hide:]
  repeated core.v3.ConfigSource config_sources = 22;

  // Default configuration source for xdstp:// URLs if all
  // other resolution fails.
  // [#not-implemented-hide:]
  core.v3.ConfigSource default_config_source = 23;

  // Optional overriding of default socket interface. The value must be the name of one of the
  // socket interface factories initialized through a bootstrap extension
  string default_socket_interface = 24;

  // Global map of CertificateProvider instances. These instances are referred to by name in the
  // :ref:`CommonTlsContext.CertificateProviderInstance.instance_name
  // <envoy_v3_api_field_extensions.transport_sockets.tls.v3.CommonTlsContext.CertificateProviderInstance.instance_name>`
  // field.
  // [#not-implemented-hide:]
  map<string, core.v3.TypedExtensionConfig> certificate_provider_instances = 25;

  // Specifies a set of headers that need to be registered as inline header. This configuration
  // allows users to customize the inline headers on-demand at Envoy startup without modifying
  // Envoy's source code.
  //
  // Note that the 'set-cookie' header cannot be registered as inline header.
  repeated CustomInlineHeader inline_headers = 32;

  // Optional path to a file with performance tracing data created by "Perfetto" SDK in binary
  // ProtoBuf format. The default value is "envoy.pftrace".
  string perf_tracing_file_path = 33;

  // Optional overriding of default regex engine.
  // If the value is not specified, Google RE2 will be used by default.
  // [#extension-category: envoy.regex_engines]
  core.v3.TypedExtensionConfig default_regex_engine = 34;

  // Optional XdsResourcesDelegate configuration, which allows plugging custom logic into both
  // fetch and load events during xDS processing.
  // If a value is not specified, no XdsResourcesDelegate will be used.
  // TODO(abeyad): Add public-facing documentation.
  // [#not-implemented-hide:]
  core.v3.TypedExtensionConfig xds_delegate_extension = 35;

  // Optional XdsConfigTracker configuration, which allows tracking xDS responses in external components,
  // e.g., external tracer or monitor. It provides the process point when receive, ingest, or fail to
  // process xDS resources and messages. If a value is not specified, no XdsConfigTracker will be used.
  //
  // .. note::
  //
  //    There are no in-repo extensions currently, and the :repo:`XdsConfigTracker <envoy/config/xds_config_tracker.h>`
  //    interface should be implemented before using.
  //    See :repo:`xds_config_tracker_integration_test <test/integration/xds_config_tracker_integration_test.cc>`
  //    for an example usage of the interface.
  core.v3.TypedExtensionConfig xds_config_tracker_extension = 36;

  // [#not-implemented-hide:]
  // This controls the type of listener manager configured for Envoy. Currently
  // Envoy only supports ListenerManager for this field and Envoy Mobile
  // supports ApiListenerManager.
  core.v3.TypedExtensionConfig listener_manager = 37;

  // Optional application log configuration.
  ApplicationLogConfig application_log_config = 38;

  // Optional gRPC async manager config.
  GrpcAsyncClientManagerConfig grpc_async_client_manager_config = 40;

  // Optional configuration for memory allocation manager.
  // Memory releasing is only supported for `tcmalloc allocator <https://github.com/google/tcmalloc>`_.
  MemoryAllocatorManager memory_allocator_manager = 41;
}


// Cluster manager :ref:`architecture overview <arch_overview_cluster_manager>`.
// [#next-free-field: 6]
message ClusterManager {

  message OutlierDetection {
    option (udpa.annotations.versioning).previous_message_type =
        "envoy.config.bootstrap.v2.ClusterManager.OutlierDetection";

    // Specifies the path to the outlier event log.
    string event_log_path = 1;

    // [#not-implemented-hide:]
    // The gRPC service for the outlier detection event service.
    // If empty, outlier detection events won't be sent to a remote endpoint.
    core.v3.EventServiceConfig event_service = 2;
  }

  // Name of the local cluster (i.e., the cluster that owns the Envoy running
  // this configuration). In order to enable :ref:`zone aware routing
  // <arch_overview_load_balancing_zone_aware_routing>` this option must be set.
  // If ``local_cluster_name`` is defined then :ref:`clusters
  // <envoy_v3_api_msg_config.cluster.v3.Cluster>` must be defined in the :ref:`Bootstrap
  // static cluster resources
  // <envoy_v3_api_field_config.bootstrap.v3.Bootstrap.StaticResources.clusters>`. This is unrelated to
  // the :option:`--service-cluster` option which does not `affect zone aware
  // routing <https://github.com/envoyproxy/envoy/issues/774>`_.
  string local_cluster_name = 1;

  // Optional global configuration for outlier detection.
  OutlierDetection outlier_detection = 2;

  // Optional configuration used to bind newly established upstream connections.
  // This may be overridden on a per-cluster basis by upstream_bind_config in the cds_config.
  core.v3.BindConfig upstream_bind_config = 3;

  // A management server endpoint to stream load stats to via
  // ``StreamLoadStats``. This must have :ref:`api_type
  // <envoy_v3_api_field_config.core.v3.ApiConfigSource.api_type>` :ref:`GRPC
  // <envoy_v3_api_enum_value_config.core.v3.ApiConfigSource.ApiType.GRPC>`.
  core.v3.ApiConfigSource load_stats_config = 4;

  // Whether the ClusterManager will create clusters on the worker threads
  // inline during requests. This will save memory and CPU cycles in cases where
  // there are lots of inactive clusters and > 1 worker thread.
  bool enable_deferred_cluster_creation = 5;
}


```