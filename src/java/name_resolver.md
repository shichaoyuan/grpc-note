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

内建的只有`io.grpc.internal.DnsNameResolverProvider`和`io.grpc.xds.XdsNameResolverProvider`等。


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

有个重要参数`nameResolverArgs.getSynchronizationContext()`

```java
/**
 * A synchronization context is a queue of tasks that run in sequence.  It offers following
 * guarantees:
 *
 * <ul>
 *    <li>Ordering.  Tasks are run in the same order as they are submitted via {@link #execute}
 *        and {@link #executeLater}.</li>
 *    <li>Serialization.  Tasks are run in sequence and establish a happens-before relationship
 *        between them. </li>
 *    <li>Non-reentrancy.  If a task running in a synchronization context executes or schedules
 *        another task in the same synchronization context, the latter task will never run
 *        inline.  It will instead be queued and run only after the current task has returned.</li>
 * </ul>
 *
 * <p>It doesn't own any thread.  Tasks are run from caller's or caller-provided threads.
 *
 * <p>Conceptually, it is fairly accurate to think of {@code SynchronizationContext} like a cheaper
 * {@code Executors.newSingleThreadExecutor()} when used for synchronization (not long-running
 * tasks). Both use a queue for tasks that are run in order and neither guarantee that tasks have
 * completed before returning from {@code execute()}. However, the behavior does diverge if locks
 * are held when calling the context. So it is encouraged to avoid mixing locks and synchronization
 * context except via {@link #executeLater}.
 *
 * <p>This class is thread-safe.
 *
 * @since 1.17.0
 */
@ThreadSafe
public final class SynchronizationContext implements Executor {
}
```

## NameResolver接口

```java
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
public abstract class NameResolver {
  /**
   * Returns the authority used to authenticate connections to servers.  It <strong>must</strong> be
   * from a trusted source, because if the authority is tampered with, RPCs may be sent to the
   * attackers which may leak sensitive user data.
   *
   * <p>An implementation must generate it without blocking, typically in line, and
   * <strong>must</strong> keep it unchanged. {@code NameResolver}s created from the same factory
   * with the same argument must return the same authority.
   *
   * @since 1.0.0
   */
  public abstract String getServiceAuthority();

  /**
   * Starts the resolution. The method is not supposed to throw any exceptions. That might cause the
   * Channel that the name resolver is serving to crash. Errors should be propagated
   * through {@link Listener#onError}.
   * 
   * <p>An instance may not be started more than once, by any overload of this method, even after
   * an intervening call to {@link #shutdown}.
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
   * Starts the resolution. The method is not supposed to throw any exceptions. That might cause the
   * Channel that the name resolver is serving to crash. Errors should be propagated
   * through {@link Listener2#onError}.
   * 
   * <p>An instance may not be started more than once, by any overload of this method, even after
   * an intervening call to {@link #shutdown}.
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
   * Re-resolve the name.
   *
   * <p>Can only be called after {@link #start} has been called.
   *
   * <p>This is only a hint. Implementation takes it as a signal but may not start resolution
   * immediately. It should never throw.
   *
   * <p>The default implementation is no-op.
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

start方法不能抛出任何异常，如果有异常改为调用listener的onError方法。

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
          if (ManagedChannelImpl.this.nameResolver != resolver) {
            return;
          }

          List<EquivalentAddressGroup> servers = resolutionResult.getAddresses();
          channelLogger.log(
              ChannelLogLevel.DEBUG,
              "Resolved address: {0}, config={1}",
              servers,
              resolutionResult.getAttributes());

          if (lastResolutionState != ResolutionState.SUCCESS) {
            channelLogger.log(ChannelLogLevel.INFO, "Address resolved: {0}", servers);
            lastResolutionState = ResolutionState.SUCCESS;
          }

          ConfigOrError configOrError = resolutionResult.getServiceConfig();
          ResolutionResultListener resolutionResultListener = resolutionResult.getAttributes()
              .get(RetryingNameResolver.RESOLUTION_RESULT_LISTENER_KEY);
          InternalConfigSelector resolvedConfigSelector =
              resolutionResult.getAttributes().get(InternalConfigSelector.KEY);
          ManagedChannelServiceConfig validServiceConfig =
              configOrError != null && configOrError.getConfig() != null
                  ? (ManagedChannelServiceConfig) configOrError.getConfig()
                  : null;
          Status serviceConfigError = configOrError != null ? configOrError.getError() : null;

          ManagedChannelServiceConfig effectiveServiceConfig;
          if (!lookUpServiceConfig) {
            if (validServiceConfig != null) {
              channelLogger.log(
                  ChannelLogLevel.INFO,
                  "Service config from name resolver discarded by channel settings");
            }
            effectiveServiceConfig =
                defaultServiceConfig == null ? EMPTY_SERVICE_CONFIG : defaultServiceConfig;
            if (resolvedConfigSelector != null) {
              channelLogger.log(
                  ChannelLogLevel.INFO,
                  "Config selector from name resolver discarded by channel settings");
            }
            realChannel.updateConfigSelector(effectiveServiceConfig.getDefaultConfigSelector());
          } else {
            // Try to use config if returned from name resolver
            // Otherwise, try to use the default config if available
            if (validServiceConfig != null) {
              effectiveServiceConfig = validServiceConfig;
              if (resolvedConfigSelector != null) {
                realChannel.updateConfigSelector(resolvedConfigSelector);
                if (effectiveServiceConfig.getDefaultConfigSelector() != null) {
                  channelLogger.log(
                      ChannelLogLevel.DEBUG,
                      "Method configs in service config will be discarded due to presence of"
                          + "config-selector");
                }
              } else {
                realChannel.updateConfigSelector(effectiveServiceConfig.getDefaultConfigSelector());
              }
            } else if (defaultServiceConfig != null) {
              effectiveServiceConfig = defaultServiceConfig;
              realChannel.updateConfigSelector(effectiveServiceConfig.getDefaultConfigSelector());
              channelLogger.log(
                  ChannelLogLevel.INFO,
                  "Received no service config, using default service config");
            } else if (serviceConfigError != null) {
              if (!serviceConfigUpdated) {
                // First DNS lookup has invalid service config, and cannot fall back to default
                channelLogger.log(
                    ChannelLogLevel.INFO,
                    "Fallback to error due to invalid first service config without default config");
                // This error could be an "inappropriate" control plane error that should not bleed
                // through to client code using gRPC. We let them flow through here to the LB as
                // we later check for these error codes when investigating pick results in
                // GrpcUtil.getTransportFromPickResult().
                onError(configOrError.getError());
                if (resolutionResultListener != null) {
                  resolutionResultListener.resolutionAttempted(configOrError.getError());
                }
                return;
              } else {
                effectiveServiceConfig = lastServiceConfig;
              }
            } else {
              effectiveServiceConfig = EMPTY_SERVICE_CONFIG;
              realChannel.updateConfigSelector(null);
            }
            if (!effectiveServiceConfig.equals(lastServiceConfig)) {
              channelLogger.log(
                  ChannelLogLevel.INFO,
                  "Service config changed{0}",
                  effectiveServiceConfig == EMPTY_SERVICE_CONFIG ? " to empty" : "");
              lastServiceConfig = effectiveServiceConfig;
              transportProvider.throttle = effectiveServiceConfig.getRetryThrottling();
            }

            try {
              // TODO(creamsoup): when `servers` is empty and lastResolutionStateCopy == SUCCESS
              //  and lbNeedAddress, it shouldn't call the handleServiceConfigUpdate. But,
              //  lbNeedAddress is not deterministic
              serviceConfigUpdated = true;
            } catch (RuntimeException re) {
              logger.log(
                  Level.WARNING,
                  "[" + getLogId() + "] Unexpected exception from parsing service config",
                  re);
            }
          }

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
