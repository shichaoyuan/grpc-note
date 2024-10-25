# XdsNameResolver(Java)

Java版本的grpc-xds使用的是ADS，资源的调用顺序与[envoy文档](https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#eventual-consistency-considerations)略有区别。

在`ManagedChannel`中`NameResolver`的起始方法是start，所以首先从`XdsNameResolver`的start方法开始。

```java
  @Override
  public void start(Listener2 listener) {
    this.listener = checkNotNull(listener, "listener");
    try {
      xdsClientPool = xdsClientPoolFactory.getOrCreate();
    } catch (Exception e) {
      listener.onError(
          Status.UNAVAILABLE.withDescription("Failed to initialize xDS").withCause(e));
      return;
    }
    xdsClient = xdsClientPool.getObject();
    BootstrapInfo bootstrapInfo = xdsClient.getBootstrapInfo();
    String listenerNameTemplate;
    if (targetAuthority == null) {
      listenerNameTemplate = bootstrapInfo.clientDefaultListenerResourceNameTemplate();
    } else {
      AuthorityInfo authorityInfo = bootstrapInfo.authorities().get(targetAuthority);
      if (authorityInfo == null) {
        listener.onError(Status.INVALID_ARGUMENT.withDescription(
            "invalid target URI: target authority not found in the bootstrap"));
        return;
      }
      listenerNameTemplate = authorityInfo.clientListenerResourceNameTemplate();
    }
    String replacement = serviceAuthority;
    if (listenerNameTemplate.startsWith(XDSTP_SCHEME)) {
      replacement = XdsClient.percentEncodePath(replacement);
    }
    String ldsResourceName = expandPercentS(listenerNameTemplate, replacement);
    if (!XdsClient.isResourceNameValid(ldsResourceName, XdsListenerResource.getInstance().typeUrl())
        ) {
      listener.onError(Status.INVALID_ARGUMENT.withDescription(
          "invalid listener resource URI for service authority: " + serviceAuthority));
      return;
    }
    ldsResourceName = XdsClient.canonifyResourceName(ldsResourceName);
    callCounterProvider = SharedCallCounterMap.getInstance();
    resolveState = new ResolveState(ldsResourceName);
    resolveState.start();
  }
```

这里构建了两个重要的对象：
1. xdsClient 封装与xDS server的交互逻辑
2. bootstrapInfo 上述客户端的配置信息

设计文档在[gRPC A27](https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md)，具体的类图如下：

![整体类图](./images/xdsnameresolver.svg)

## Bootstrapper

`Bootstrapper`封装的是获取配置的逻辑，默认情况下的实现是`GrpcBootstrapperImpl`。

通常来说一般在`XdsNameResolverProvider`中不会设置bootstrapOverride，那么在`GrpcBootstrapperImpl`中维护的就是json文件的内容，也就是程序全局的配置。

```java
  protected BootstrapInfo.Builder bootstrapBuilder(Map<String, ?> rawData)
      throws XdsInitializationException {
    BootstrapInfo.Builder builder = BootstrapInfo.builder();

    List<?> rawServerConfigs = JsonUtil.getList(rawData, "xds_servers");
    if (rawServerConfigs == null) {
      throw new XdsInitializationException("Invalid bootstrap: 'xds_servers' does not exist.");
    }
    List<ServerInfo> servers = parseServerInfos(rawServerConfigs, logger);
    builder.servers(servers);

    Node.Builder nodeBuilder = Node.newBuilder();
    Map<String, ?> rawNode = JsonUtil.getObject(rawData, "node");
    if (rawNode != null) {
      String id = JsonUtil.getString(rawNode, "id");
      if (id != null) {
        logger.log(XdsLogLevel.INFO, "Node id: {0}", id);
        nodeBuilder.setId(id);
      }
      String cluster = JsonUtil.getString(rawNode, "cluster");
      if (cluster != null) {
        logger.log(XdsLogLevel.INFO, "Node cluster: {0}", cluster);
        nodeBuilder.setCluster(cluster);
      }
      Map<String, ?> metadata = JsonUtil.getObject(rawNode, "metadata");
      if (metadata != null) {
        nodeBuilder.setMetadata(metadata);
      }
      Map<String, ?> rawLocality = JsonUtil.getObject(rawNode, "locality");
      if (rawLocality != null) {
        String region = "";
        String zone = "";
        String subZone = "";
        if (rawLocality.containsKey("region")) {
          region = JsonUtil.getString(rawLocality, "region");
        }
        if (rawLocality.containsKey("zone")) {
          zone = JsonUtil.getString(rawLocality, "zone");
        }
        if (rawLocality.containsKey("sub_zone")) {
          subZone = JsonUtil.getString(rawLocality, "sub_zone");
        }
        logger.log(XdsLogLevel.INFO, "Locality region: {0}, zone: {1}, subZone: {2}",
            region, zone, subZone);
        Locality locality = Locality.create(region, zone, subZone);
        nodeBuilder.setLocality(locality);
      }
    }
    GrpcBuildVersion buildVersion = GrpcUtil.getGrpcBuildVersion();
    logger.log(XdsLogLevel.INFO, "Build version: {0}", buildVersion);
    nodeBuilder.setBuildVersion(buildVersion.toString());
    nodeBuilder.setUserAgentName(buildVersion.getUserAgent());
    nodeBuilder.setUserAgentVersion(buildVersion.getImplementationVersion());
    nodeBuilder.addClientFeatures(CLIENT_FEATURE_DISABLE_OVERPROVISIONING);
    nodeBuilder.addClientFeatures(CLIENT_FEATURE_RESOURCE_IN_SOTW);
    builder.node(nodeBuilder.build());

    Map<String, ?> certProvidersBlob = JsonUtil.getObject(rawData, "certificate_providers");
    if (certProvidersBlob != null) {
      logger.log(XdsLogLevel.INFO, "Configured with {0} cert providers", certProvidersBlob.size());
      Map<String, CertificateProviderInfo> certProviders = new HashMap<>(certProvidersBlob.size());
      for (String name : certProvidersBlob.keySet()) {
        Map<String, ?> valueMap = JsonUtil.getObject(certProvidersBlob, name);
        String pluginName =
            checkForNull(JsonUtil.getString(valueMap, "plugin_name"), "plugin_name");
        logger.log(XdsLogLevel.INFO, "cert provider: {0}, plugin name: {1}", name, pluginName);
        Map<String, ?> config = checkForNull(JsonUtil.getObject(valueMap, "config"), "config");
        CertificateProviderInfo certificateProviderInfo =
            CertificateProviderInfo.create(pluginName, config);
        certProviders.put(name, certificateProviderInfo);
      }
      builder.certProviders(certProviders);
    }

    String serverResourceId =
        JsonUtil.getString(rawData, "server_listener_resource_name_template");
    logger.log(
        XdsLogLevel.INFO, "server_listener_resource_name_template: {0}", serverResourceId);
    builder.serverListenerResourceNameTemplate(serverResourceId);

    String clientDefaultListener =
        JsonUtil.getString(rawData, "client_default_listener_resource_name_template");
    logger.log(
        XdsLogLevel.INFO, "client_default_listener_resource_name_template: {0}",
        clientDefaultListener);
    if (clientDefaultListener != null) {
      builder.clientDefaultListenerResourceNameTemplate(clientDefaultListener);
    }

    Map<String, ?> rawAuthoritiesMap =
        JsonUtil.getObject(rawData, "authorities");
    ImmutableMap.Builder<String, AuthorityInfo> authorityInfoMapBuilder = ImmutableMap.builder();
    if (rawAuthoritiesMap != null) {
      logger.log(
          XdsLogLevel.INFO, "Configured with {0} xDS server authorities", rawAuthoritiesMap.size());
      for (String authorityName : rawAuthoritiesMap.keySet()) {
        logger.log(XdsLogLevel.INFO, "xDS server authority: {0}", authorityName);
        Map<String, ?> rawAuthority = JsonUtil.getObject(rawAuthoritiesMap, authorityName);
        String clientListnerTemplate =
            JsonUtil.getString(rawAuthority, "client_listener_resource_name_template");
        logger.log(
            XdsLogLevel.INFO, "client_listener_resource_name_template: {0}", clientListnerTemplate);
        String prefix = XDSTP_SCHEME + "//" + authorityName + "/";
        if (clientListnerTemplate == null) {
          clientListnerTemplate = prefix + "envoy.config.listener.v3.Listener/%s";
        } else if (!clientListnerTemplate.startsWith(prefix)) {
          throw new XdsInitializationException(
              "client_listener_resource_name_template: '" + clientListnerTemplate
                  + "' does not start with " + prefix);
        }
        List<?> rawAuthorityServers = JsonUtil.getList(rawAuthority, "xds_servers");
        List<ServerInfo> authorityServers;
        if (rawAuthorityServers == null || rawAuthorityServers.isEmpty()) {
          authorityServers = servers;
        } else {
          authorityServers = parseServerInfos(rawAuthorityServers, logger);
        }
        authorityInfoMapBuilder.put(
            authorityName, AuthorityInfo.create(clientListnerTemplate, authorityServers));
      }
      builder.authorities(authorityInfoMapBuilder.buildOrThrow());
    }

    return builder;
  }
```

解析后的`BootstrapInfo`主要包含：
```json
{
  "xds_servers": [
    {
      "server_uri": "value",
      "channel_creds": [
        {
          "type": "value",
          "config" {}
        }
      ],
      "server_features": [
        "value"
      ]
    }
  ],
  "node": {
    "id": "value",
    "cluster": "value",
    "metadata": {},
    "locality": {
      "region": "value",
      "zone": "value",
      "sub_zone": "value"
    }
  },
  "certificate_providers": {
    "name": {
      "plugin_name": "value",
      "config": {}
    }
  },
  "server_listener_resource_name_template": "value",
  "client_default_listener_resource_name_template": "value",
  "authorities": {
    "name": {
      "client_listener_resource_name_template": "value",
      "xds_servers": [
      ]
    }
  }
}
```
在authorities中，client_listener_resource_name_template如果没有设置，那么默认值为xdstp://authorityName/envoy.config.listener.v3.Listener/%s；client_default_listener_resource_name_template的默认值是%s。

实际上authorities的引入比较晚，在[gRPC A47](https://github.com/grpc/proposal/blob/master/A47-xds-federation.md)中，主要是为了支持不同的资源能够访问不同的xDS服务器，没有这一特性之前只能访问默认的xds_servers。

这两个信息影响着`XdsNameResolver`在start中的`ldsResourceName`的设定，假设设置的target为xds:///greeter-s003，那么`targetAuthority`就是空的，`serviceAuthority`就是greeter-s003，这里总结一些不同情况的处理：
1. targetAuthority为空
  1. clientDefaultListenerResourceNameTemplate为默认值，那么ldsResourceName就是`serviceAuthority`
  2. 如果是以xdstp:开头，那么ldsResourceName就是xdstp:///envoy.config.listener.v3.Listener/xxx
2. targetAuthority不为空
  1. 那么从authorities中匹配clientListenerResourceNameTemplate，然后逻辑与上面一致

## ResolveState

在`start`过程中首先监听的是LDS。

```java
    private void start() {
      logger.log(XdsLogLevel.INFO, "Start watching LDS resource {0}", ldsResourceName);
      xdsClient.watchXdsResource(XdsListenerResource.getInstance(),
          ldsResourceName, this, syncContext);
    }
```

`watchXdsResource`是监听xds变更的通用方法，其第一个参数就是资源类型，目前在代码中实现了四种资源类型：
1. `XdsListenerResource` "type.googleapis.com/envoy.config.listener.v3.Listener"
2. `XdsRouteConfigureResource` "type.googleapis.com/envoy.config.route.v3.RouteConfiguration"
3. `XdsClusterResource` "type.googleapis.com/envoy.config.cluster.v3.Cluster"
4. `XdsEndpointResource` "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment"

在`XdsClientImpl`使用了一个Map维护订阅者的关系：
```java
  @Override
  public <T extends ResourceUpdate> void watchXdsResource(XdsResourceType<T> type,
      String resourceName,
      ResourceWatcher<T> watcher,
      Executor watcherExecutor) {
    syncContext.execute(new Runnable() {
      @Override
      @SuppressWarnings("unchecked")
      public void run() {
        if (!resourceSubscribers.containsKey(type)) {
          resourceSubscribers.put(type, new HashMap<>());
          subscribedResourceTypeUrls.put(type.typeUrl(), type);
        }
        ResourceSubscriber<T> subscriber =
            (ResourceSubscriber<T>) resourceSubscribers.get(type).get(resourceName);
        if (subscriber == null) {
          logger.log(XdsLogLevel.INFO, "Subscribe {0} resource {1}", type, resourceName);
          subscriber = new ResourceSubscriber<>(type, resourceName);
          resourceSubscribers.get(type).put(resourceName, subscriber);
          if (subscriber.controlPlaneClient != null) {
            subscriber.controlPlaneClient.adjustResourceSubscription(type);
          }
        }
        subscriber.addWatcher(watcher, watcherExecutor);
      }
    });
  }
```
也就是每个资源有唯一一个对应的`ResourceSubscriber`，但是每个subscriber可以添加多个`ResourceWatcher`。

在构建`ResourceSubscriber`时最重要的从配置中获取对应的xdsServer，然后构建具体的controlPlaneClient。

controlPlaneClient的使用是在`ResourceSubscriber`，但是维护还是在`XdsClientImpl`中的serverCpClientMap，每个xdsServer对应一个单例。

## ControlPlaneClient

在`ControlPlaneClient`中对于xds连接的封装是`GrpcXdsTransport`，实际上就是`ManagedChannel`，在此之上`AdsStream`分装了协议相关的处理逻辑。

ADS协议是在一个stream上订阅所有的资源，所以当变更时需要重新订阅，也就是`adjustResourceSubscription`,adsStream.sendDiscoveryRequest发送请求后，正常情况将会收到响应进入`handleRpcResponse`处理逻辑：

```java
    final void handleRpcResponse(XdsResourceType<?> type, String versionInfo, List<Any> resources,
                                 String nonce) {
      checkNotNull(type, "type");
      if (closed) {
        return;
      }
      responseReceived = true;
      respNonces.put(type, nonce);
      ProcessingTracker processingTracker = new ProcessingTracker(
          () -> call.startRecvMessage(), syncContext);
      xdsResponseHandler.handleResourceResponse(type, serverInfo, versionInfo, resources, nonce,
          processingTracker);
      processingTracker.onComplete();
    }
```
`handleResourceResponse`的逻辑回调到了`XdsClientImpl`中，在这里会根据解析的结果再调用`ControlPlaneClient`进行ack或者nack。

对于异常情况，在`ControlPlaneClient`中有个`rpcRetryTimer`驱动进行重试，创建新的stream，发送DS请求。
