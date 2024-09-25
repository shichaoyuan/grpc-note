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

  // Statically specified resources.
  StaticResources static_resources = 2;
  // xDS configuration sources.
  DynamicResources dynamic_resources = 3;

  // 本地管理HTTP服务器
  Admin admin = 12;

}
```

xdstp是cncf提供的基于URI的xDS资源名格式，详见[TP1-xds-transport-next.md](https://github.com/cncf/xds/blob/main/proposals/TP1-xds-transport-next.md)

>`xdstp://[{authority}]/{resource type}/{id/*}?{context parameters}{#processing directive,*}`
>
>* `authority` is an opaque string naming a resource authority, e.g. `some.control.plane`.
>* `resource type` is the xDS resource type URL, e.g. `envoy.config.route.v3.RouteConfiguration`
>* `id/*` is the remaining path component of the URI and is fully opaque; naming is at the discretion of the control plane(s).
>* `context parameters` are the URI query parameters and express contextual information for selecting resource variants, for example `shard_id=1234&direction=inbound`.
>* A `processing directive` provides additional information to the client on how the URI is to be interpreted. Two supported directives are:
>  * `alt=<xdstp:// URI>` expressing a fallback URI.
>  * `entry=<resource name>` providing an anchor referring to an inlined resource for [list collections](#list). Resource names must be of the form `[a-zA-Z0-9_-\./]+`.

## Extension配置

每个资源都需要指定类型，有两种方式：
```yaml
      - name: front-http-proxy
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          stat_prefix: ingress_http
          codec_type: AUTO
```
```yaml
      - name: front-http-proxy
        typed_config:
          "@type": type.googleapis.com/xds.type.v3.TypedStruct
          type_url: type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          value:
            stat_prefix: ingress_http
            codec_type: AUTO
```

### ECDS

//todo

### filter

这些资源主要以filter的形式提供：
* Listener filters 位于 `envoy.extensions.filters.listener`
* Network filters 位于 `envoy.extensions.filters.network`
* HTTP filters 位于 `envoy.extensions.filters.http`
* UDP Session filters 位于 `envoy.extensions.filters.udp`

## xDS API endpoints

v3 transport API 的 endpoints，这些proto3定义文件位于`/envoy/service`目录下。

### gRPC streaming endpoints

**POST /envoy.service.cluster.v3.ClusterDiscoveryService/StreamClusters**

在dynamic_resources中配置cds调用的集群
```yaml
dynamic_resources:
  cds_config:
    api_config_source:
      api_type: GRPC
      grpc_services:
      - envoy_grpc:
          cluster_name: xds_cluster
```

**POST /envoy.service.endpoint.v3.EndpointDiscoveryService/StreamEndpoints**

eds的配置是在cds返回的`Cluster`配置中
```yaml
eds_config:
  api_config_source:
    api_type: GRPC
    grpc_services:
    - envoy_grpc:
        cluster_name: some_xds_cluster
```

**POST /envoy.service.listener.v3.ListenerDiscoveryService/StreamListeners**

在dynamic_resources中配置lds调用的集群
```yaml
dynamic_resources:
  lds_config:
    api_config_source:
      api_type: GRPC
      grpc_services:
      - envoy_grpc:
          cluster_name: xds_cluster
```

**POST /envoy.service.route.v3.RouteDiscoveryService/StreamRoutes**

rds的配置是在lds返回的`HttpConnectionManager`配置中
```yaml
route_config_name: some_route_name
config_source:
  api_config_source:
    api_type: GRPC
    grpc_services:
    - envoy_grpc:
        cluster_name: some_xds_cluster
```
**POST /envoy.service.route.v3.ScopedRoutesDiscoveryService/StreamScopedRoutes**

srds的配置也是在lds返回的`HttpConnectionManager`配置中
```yaml
name: some_scoped_route_name
scoped_rds:
  config_source:
    api_config_source:
      api_type: GRPC
      grpc_services:
      - envoy_grpc:
          cluster_name: some_xds_cluster
```

**POST /envoy.service.secret.v3.SecretDiscoveryService/StreamSecrets**

sds的配置存在很多地方

**POST /envoy.service.runtime.v3.RuntimeDiscoveryService/StreamRuntime**

rtds配置在bootstrap的rtds_layer中
```yaml
name: some_runtime_layer_name
config_source:
  api_config_source:
    api_type: GRPC
    grpc_services:
    - envoy_grpc:
        cluster_name: some_xds_cluster
```
### REST endpoints

* POST /v3/discovery:clusters
* POST /v3/discovery:endpoints
* POST /v3/discovery:listeners
* POST /v3/discovery:routes

### Aggregated Discovery Service

按照上面分割的接口，Envoy只能实现配置的最终一致性。

统一ADS接口提供了序列化API更新推送的可能。

**POST /envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources**

在dynamic_resources中配置ads
```yaml
dynamic_resources:
  ads_config:
    api_type: GRPC
    grpc_services:
    - envoy_grpc:
        cluster_name: xds_cluster
  cds_config: {ads: {}}
  lds_config: {ads: {}}
```

### Delta endpoints

// todo

### xDS TTL
