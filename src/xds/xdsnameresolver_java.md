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












