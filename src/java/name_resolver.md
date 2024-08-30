# NameResolver


## NameResolverProvider.newNameResolver 

```java
public abstract class ManagedChannelBuilder<T extends ManagedChannelBuilder<T>> {

  public static ManagedChannelBuilder<?> forTarget(String target) {
    return ManagedChannelProvider.provider().builderForTarget(target);
  }

}
```

默认提供的是priority为5的`io.grpc.netty.NettyChannelProvider`

```java
public final class ManagedChannelImplBuilder
    extends ManagedChannelBuilder<ManagedChannelImplBuilder> {

  @Override
  public ManagedChannel build() {
    ClientTransportFactory clientTransportFactory =
        clientTransportFactoryBuilder.buildClientTransportFactory();
    ResolvedNameResolver resolvedResolver = getNameResolverProvider(
        target, nameResolverRegistry, clientTransportFactory.getSupportedSocketAddressTypes());
    return new ManagedChannelOrphanWrapper(new ManagedChannelImpl(
        this,
        clientTransportFactory,
        resolvedResolver.targetUri,
        resolvedResolver.provider,
        new ExponentialBackoffPolicy.Provider(),
        SharedResourcePool.forResource(GrpcUtil.SHARED_CHANNEL_EXECUTOR),
        GrpcUtil.STOPWATCH_SUPPLIER,
        getEffectiveInterceptors(resolvedResolver.targetUri.toString()),
        TimeProvider.SYSTEM_TIME_PROVIDER));
  }

  static ResolvedNameResolver getNameResolverProvider(
      String target, NameResolverRegistry nameResolverRegistry,
      Collection<Class<? extends SocketAddress>> channelTransportSocketAddressTypes) {
    // Finding a NameResolver. Try using the target string as the URI. If that fails, try prepending
    // "dns:///".
    NameResolverProvider provider = null;
    URI targetUri = null;
    StringBuilder uriSyntaxErrors = new StringBuilder();
    try {
      targetUri = new URI(target);
    } catch (URISyntaxException e) {
      // Can happen with ip addresses like "[::1]:1234" or 127.0.0.1:1234.
      uriSyntaxErrors.append(e.getMessage());
    }
    if (targetUri != null) {
      // For "localhost:8080" this would likely cause provider to be null, because "localhost" is
      // parsed as the scheme. Will hit the next case and try "dns:///localhost:8080".
      provider = nameResolverRegistry.getProviderForScheme(targetUri.getScheme());
    }

    if (provider == null && !URI_PATTERN.matcher(target).matches()) {
      // It doesn't look like a URI target. Maybe it's an authority string. Try with the default
      // scheme from the registry.
      try {
        targetUri = new URI(nameResolverRegistry.getDefaultScheme(), "", "/" + target, null);
      } catch (URISyntaxException e) {
        // Should not be possible.
        throw new IllegalArgumentException(e);
      }
      provider = nameResolverRegistry.getProviderForScheme(targetUri.getScheme());
    }

    if (provider == null) {
      throw new IllegalArgumentException(String.format(
          "Could not find a NameResolverProvider for %s%s",
          target, uriSyntaxErrors.length() > 0 ? " (" + uriSyntaxErrors + ")" : ""));
    }

    if (channelTransportSocketAddressTypes != null) {
      Collection<Class<? extends SocketAddress>> nameResolverSocketAddressTypes
          = provider.getProducedSocketAddressTypes();
      if (!channelTransportSocketAddressTypes.containsAll(nameResolverSocketAddressTypes)) {
        throw new IllegalArgumentException(String.format(
            "Address types of NameResolver '%s' for '%s' not supported by transport",
            targetUri.getScheme(), target));
      }
    }

    return new ResolvedNameResolver(targetUri, provider);
  }


}
```

其中`resolvedResolver.provider`是根据scheme获取的`io.grpc.NameResolverProvider`。

内建的有`io.grpc.internal.DnsNameResolverProvider`和`io.grpc.xds.XdsNameResolverProvider`等。


```java
@ThreadSafe
final class ManagedChannelImpl extends ManagedChannel implements
    InternalInstrumented<ChannelStats> {
  
  @VisibleForTesting
  final SynchronizationContext syncContext = new SynchronizationContext(
      new Thread.UncaughtExceptionHandler() {
        @Override
        public void uncaughtException(Thread t, Throwable e) {
          logger.log(
              Level.SEVERE,
              "[" + getLogId() + "] Uncaught exception in the SynchronizationContext. Panic!",
              e);
          panic(e);
        }
      });


  static NameResolver getNameResolver(
      URI targetUri, @Nullable final String overrideAuthority,
      NameResolverProvider provider, NameResolver.Args nameResolverArgs) {
    NameResolver resolver = provider.newNameResolver(targetUri, nameResolverArgs);
    if (resolver == null) {
      throw new IllegalArgumentException("cannot create a NameResolver for " + targetUri);
    }

    // We wrap the name resolver in a RetryingNameResolver to give it the ability to retry failures.
    // TODO: After a transition period, all NameResolver implementations that need retry should use
    //       RetryingNameResolver directly and this step can be removed.
    NameResolver usedNameResolver = new RetryingNameResolver(resolver,
          new BackoffPolicyRetryScheduler(new ExponentialBackoffPolicy.Provider(),
              nameResolverArgs.getScheduledExecutorService(),
              nameResolverArgs.getSynchronizationContext()),
          nameResolverArgs.getSynchronizationContext());

    if (overrideAuthority == null) {
      return usedNameResolver;
    }

    return new ForwardingNameResolver(usedNameResolver) {
      @Override
      public String getServiceAuthority() {
        return overrideAuthority;
      }
    };
  }

}
```

有个重要参数`nameResolverArgs.getSynchronizationContext()`，可以理解为轻量的`Executors.newSingleThreadExecutor()`。

## NameResolver接口

```java
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
public abstract class NameResolver {

  /**
   * 启动地址解析，改方法不能抛出任何异常，只能通过Listener#onError方法传递。
   * 
   * 一个实例只会被start一次。
   *
   * @param listener used to receive updates on the target
   * @since 1.0.0
   */
  public void start(final Listener listener) {
    if (listener instanceof Listener2) {
      start((Listener2) listener);
    } else {
      start(new Listener2() {
          @Override
          public void onError(Status error) {
            listener.onError(error);
          }

          @Override
          public void onResult(ResolutionResult resolutionResult) {
            listener.onAddresses(resolutionResult.getAddresses(), resolutionResult.getAttributes());
          }
      });
    }
  }

  /**
   * 同上
   *
   * @param listener used to receive updates on the target
   * @since 1.21.0
   */
  public void start(Listener2 listener) {
    start((Listener) listener);
  }

  /**
   * Stops the resolution. Updates to the Listener will stop.
   *
   * @since 1.0.0
   */
  public abstract void shutdown();

  /**
   * 重新解析地址
   *
   * 只能在start之后调用
   *
   * 这只是个信号，而不是直接进行地址解析。这个也不能抛出异常
   *
   * 默认的实现是什么都不做
   *
   * @since 1.0.0
   */
  public void refresh() {}

}
```

## NameResolver.start

```log
-> io.grpc.stub.ClientCalls#blockingUnaryCall(io.grpc.Channel, io.grpc.MethodDescriptor<ReqT,RespT>, io.grpc.CallOptions, ReqT)
  -> io.grpc.internal.ForwardingManagedChannel#newCall
    -> io.grpc.internal.ManagedChannelImpl#newCall
      -> io.grpc.internal.ManagedChannelImpl.RealChannel#newCall
        -> io.grpc.SynchronizationContext#execute
          -> io.grpc.internal.ManagedChannelImpl#exitIdleMode
            -> io.grpc.internal.RetryingNameResolver#start
              -> io.grpc.internal.ForwardingNameResolver#start(io.grpc.NameResolver.Listener2)
                -> XXOONameResolver#start
```

在ManagedChannelImpl构建后`channelStateManager`默认是IDLE状态，然后在第一次调用时触发`exitIdleMode`转为CONNECTING状态。

```java
@ThreadSafe
final class ManagedChannelImpl extends ManagedChannel implements
    InternalInstrumented<ChannelStats> {

  @VisibleForTesting
  void exitIdleMode() {
    syncContext.throwIfNotInThisSynchronizationContext();
    if (shutdown.get() || panicMode) {
      return;
    }
    if (inUseStateAggregator.isInUse()) {
      // Cancel the timer now, so that a racing due timer will not put Channel on idleness
      // when the caller of exitIdleMode() is about to use the returned loadBalancer.
      cancelIdleTimer(false);
    } else {
      // exitIdleMode() may be called outside of inUseStateAggregator.handleNotInUse() while
      // isInUse() == false, in which case we still need to schedule the timer.
      rescheduleIdleTimer();
    }
    if (lbHelper != null) {
      return;
    }
    channelLogger.log(ChannelLogLevel.INFO, "Exiting idle mode");
    LbHelperImpl lbHelper = new LbHelperImpl();
    lbHelper.lb = loadBalancerFactory.newLoadBalancer(lbHelper);
    // Delay setting lbHelper until fully initialized, since loadBalancerFactory is user code and
    // may throw. We don't want to confuse our state, even if we will enter panic mode.
    this.lbHelper = lbHelper;

    channelStateManager.gotoState(CONNECTING);
    NameResolverListener listener = new NameResolverListener(lbHelper, nameResolver);
    nameResolver.start(listener);
    nameResolverStarted = true;
  }

}
```

控制start方法只能被调用一次的居然是`lbHelper`的构建，给一个明确的标识可能会更清晰。

## NameResolverListener

```java
  final class NameResolverListener extends NameResolver.Listener2 {
    final LbHelperImpl helper;
    final NameResolver resolver;

    NameResolverListener(LbHelperImpl helperImpl, NameResolver resolver) {
      this.helper = checkNotNull(helperImpl, "helperImpl");
      this.resolver = checkNotNull(resolver, "resolver");
    }

    @Override
    public void onResult(final ResolutionResult resolutionResult) {
      final class NamesResolved implements Runnable {

        @SuppressWarnings("ReferenceEquality")
        @Override
        public void run() {

          Attributes effectiveAttrs = resolutionResult.getAttributes();
          // Call LB only if it's not shutdown.  If LB is shutdown, lbHelper won't match.
          if (NameResolverListener.this.helper == ManagedChannelImpl.this.lbHelper) {
            Attributes.Builder attrBuilder =
                effectiveAttrs.toBuilder().discard(InternalConfigSelector.KEY);
            Map<String, ?> healthCheckingConfig =
                effectiveServiceConfig.getHealthCheckingConfig();
            if (healthCheckingConfig != null) {
              attrBuilder
                  .set(LoadBalancer.ATTR_HEALTH_CHECKING_CONFIG, healthCheckingConfig)
                  .build();
            }
            Attributes attributes = attrBuilder.build();

            Status addressAcceptanceStatus = helper.lb.tryAcceptResolvedAddresses(
                ResolvedAddresses.newBuilder()
                    .setAddresses(servers)
                    .setAttributes(attributes)
                    .setLoadBalancingPolicyConfig(effectiveServiceConfig.getLoadBalancingConfig())
                    .build());
            // If a listener is provided, let it know if the addresses were accepted.
            if (resolutionResultListener != null) {
              resolutionResultListener.resolutionAttempted(addressAcceptanceStatus);
            }
          }
        }
      }

      syncContext.execute(new NamesResolved());
    }

    @Override
    public void onError(final Status error) {
      checkArgument(!error.isOk(), "the error status must not be OK");
      final class NameResolverErrorHandler implements Runnable {
        @Override
        public void run() {
          handleErrorInSyncContext(error);
        }
      }

      syncContext.execute(new NameResolverErrorHandler());
    }

    private void handleErrorInSyncContext(Status error) {
      logger.log(Level.WARNING, "[{0}] Failed to resolve name. status={1}",
          new Object[] {getLogId(), error});
      realChannel.onConfigError();
      if (lastResolutionState != ResolutionState.ERROR) {
        channelLogger.log(ChannelLogLevel.WARNING, "Failed to resolve name: {0}", error);
        lastResolutionState = ResolutionState.ERROR;
      }
      // Call LB only if it's not shutdown.  If LB is shutdown, lbHelper won't match.
      if (NameResolverListener.this.helper != ManagedChannelImpl.this.lbHelper) {
        return;
      }

      helper.lb.handleNameResolutionError(error);
    }
  }

```

获取到解析的结果后回调listener的onResult方法，如果失败了回调onError，无论哪种情况最终都会通知到`helper.lb`，也就是负载均衡器。

```java
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
public abstract class NameResolver {

    @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
    public static final class ResolutionResult {
        private final List<EquivalentAddressGroup> addresses;
        private final Attributes attributes;
        @Nullable
        private final ConfigOrError serviceConfig;
    }

}
```

在解析的结果中出了常规的地址和属性，还有一个`serviceConfig`服务配置信息，这个信息的组织有很大的自由度。

控制serviceConfig处理的参数: `lookUpServiceConfig`是否解析服务配置，默认为true，如果设为false的话，将会使用`defaultServiceConfig`或`EMPTY_SERVICE_CONFIG`。

serviceConfig的使用比较分散，不像addresses和attributes只传给了 lb：
* RetryThrottling 传给了`transportProvider.throttle`
* DefaultConfigSelector 传给了`realChannel.updateConfigSelector`
* LoadBalancingConfig 设置为了ResolvedAddresses的`LoadBalancingPolicyConfig`
* HealthCheckingConfig 设置为了ResolvedAddresses的一个attribute

## idleTimer

一般情况`NameResolver`只会构建一个，但是也有例外的情况，如果设置了`idleTimeoutMillis`，那么在超过了该时间后就会回到IDLE状态。

实现idle状态切换主要依赖两个组件：
1. inUseStateAggregator (IdleModeStateAggregator) 用于追踪channel是否在使用，如果从0->1在使用回调`handleInUse()`，如果从1->0不用了回调`handleNotInUse()`
2. idleTimer (Rescheduler) 计时器，设置`idleTimeoutMillis`延迟，到达时间了将会回调`IdleModeTimer`


```java
  /**
   * Must be accessed from syncContext.
   */
  private final class IdleModeStateAggregator extends InUseStateAggregator<Object> {
    @Override
    protected void handleInUse() {
      exitIdleMode();
    }

    @Override
    protected void handleNotInUse() {
      if (shutdown.get()) {
        return;
      }
      rescheduleIdleTimer();
    }
  }

  // Run from syncContext
  private class IdleModeTimer implements Runnable {

    @Override
    public void run() {
      // Workaround timer scheduled while in idle mode. This can happen from handleNotInUse() after
      // an explicit enterIdleMode() by the user. Protecting here as other locations are a bit too
      // subtle to change rapidly to resolve the channel panic. See #8714
      if (lbHelper == null) {
        return;
      }
      enterIdleMode();
    }
  }
```

虽然这个idle检测默认是关闭的，但是比较优雅的`NameResolver`的实现需要在提供start开启，shutdown关闭的对称逻辑。


## refresh

提示`NameResolver`进行refresh，通常来说这是个空实现，因为大部分地址发现都是监听机制，有变更就回调listener了。

有两处地方调用refresh：
1. `RetryingNameResolver` 


