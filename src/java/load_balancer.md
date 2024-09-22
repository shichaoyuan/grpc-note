# LoadBalancer

## LbHelperImpl

由`NameResolver`了解到`lbHelper`这个字段标识着Channel状态是否离开IDLE状态。

`LbHelperImpl`实现了`LoadBalancer.Helper`，提供了负载均衡器的一些基础实现。
```java
  @ThreadSafe
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
  public abstract static class Helper {
    /**
     * 创建SubChannel，也就是一组等同地址的逻辑连接
     * 
     * attrs是关联SubChannel的自定义属性 
     *
     * 该方法只能通过 Synchronization Context 调用
     *
     * @return 不能返回null
     *
     * @since 1.22.0
     */
    public Subchannel createSubchannel(CreateSubchannelArgs args) {
      throw new UnsupportedOperationException();
    }

    /**
     * Out-of-boand channel
     * 
     * 例如外部的负载均衡器服务
     *
     * @since 1.4.0
     */
    public abstract ManagedChannel createOobChannel(EquivalentAddressGroup eag, String authority);

    /**
     * 设置新的状态，同时提供新的picker
     *
     * 该方法只能通过 Synchronization Context 调用
     *
     * 不能传入 SHUTDOWN 状态
     *
     * @since 1.6.0
     */
    public abstract void updateBalancingState(
        @Nonnull ConnectivityState newState, @Nonnull SubchannelPicker newPicker);

    /**
     * 调用 NameResolver#refresh
     *
     * 该方法只能通过 Synchronization Context 调用
     *
     * @since 1.18.0
     */
    public void refreshNameResolution() {
      throw new UnsupportedOperationException();
    }

  }
```
`LbHelperImpl`中的lb的类型为`AutoConfiguredLoadBalancer`，包装了定制化的负载均衡逻辑实现。



## AutoConfiguredLoadBalancer

```java
  public final class AutoConfiguredLoadBalancer {
    private final Helper helper;
    private LoadBalancer delegate;
    private LoadBalancerProvider delegateProvider;

    AutoConfiguredLoadBalancer(Helper helper) {
      this.helper = helper;
      delegateProvider = registry.getProvider(defaultPolicy);
      if (delegateProvider == null) {
        throw new IllegalStateException("Could not find policy '" + defaultPolicy
            + "'. Make sure its implementation is either registered to LoadBalancerRegistry or"
            + " included in META-INF/services/io.grpc.LoadBalancerProvider from your jar files.");
      }
      delegate = delegateProvider.newLoadBalancer(helper);
    }

    /**
     * Returns non-OK status if the delegate rejects the resolvedAddresses (e.g. if it does not
     * support an empty list).
     */
    Status tryAcceptResolvedAddresses(ResolvedAddresses resolvedAddresses) {
      PolicySelection policySelection =
          (PolicySelection) resolvedAddresses.getLoadBalancingPolicyConfig();

      if (policySelection == null) {
        LoadBalancerProvider defaultProvider;
        try {
          defaultProvider = getProviderOrThrow(defaultPolicy, "using default policy");
        } catch (PolicyException e) {
          Status s = Status.INTERNAL.withDescription(e.getMessage());
          helper.updateBalancingState(ConnectivityState.TRANSIENT_FAILURE, new FailingPicker(s));
          delegate.shutdown();
          delegateProvider = null;
          delegate = new NoopLoadBalancer();
          return Status.OK;
        }
        policySelection =
            new PolicySelection(defaultProvider, /* config= */ null);
      }

      if (delegateProvider == null
          || !policySelection.provider.getPolicyName().equals(delegateProvider.getPolicyName())) {
        helper.updateBalancingState(ConnectivityState.CONNECTING, new EmptyPicker());
        delegate.shutdown();
        delegateProvider = policySelection.provider;
        LoadBalancer old = delegate;
        delegate = delegateProvider.newLoadBalancer(helper);
        helper.getChannelLogger().log(
            ChannelLogLevel.INFO, "Load balancer changed from {0} to {1}",
            old.getClass().getSimpleName(), delegate.getClass().getSimpleName());
      }
      Object lbConfig = policySelection.config;
      if (lbConfig != null) {
        helper.getChannelLogger().log(tryAcceptResolvedAddresses
            ChannelLogLevel.DEBUG, "Load-balancing config: {0}", policySelection.config);
      }

      return getDelegate().acceptResolvedAddresses(
          ResolvedAddresses.newBuilder()
              .setAddresses(resolvedAddresses.getAddresses())
              .setAttributes(resolvedAddresses.getAttributes())
              .setLoadBalancingPolicyConfig(lbConfig)
              .build());
    }
  }
```

这里的`tryAcceptResolvedAddresses`方法就是`NameResolverListener.onResult`中传递地址列表的方法。

`policySelection`来自servicConfig，基于传递的policy可以实现动态切换`LoadBalancer`。


## LoadBalancer

LoadBalancer通常情况需要实现这几个方法。

```java
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
@NotThreadSafe
public abstract class LoadBalancer {

  /**
   * Accepts newly resolved addresses from the name resolution system. The {@link
   * EquivalentAddressGroup} addresses should be considered equivalent but may be flattened into a
   * single list if needed.
   *
   * <p>Implementations can choose to reject the given addresses by returning {@code false}.
   *
   * <p>Implementations should not modify the given {@code addresses}.
   *
   * @param resolvedAddresses the resolved server addresses, attributes, and config.
   * @return {@code true} if the resolved addresses were accepted. {@code false} if rejected.
   * @since 1.49.0
   */
  public Status acceptResolvedAddresses(ResolvedAddresses resolvedAddresses) {
    if (resolvedAddresses.getAddresses().isEmpty()
        && !canHandleEmptyAddressListFromNameResolution()) {
      Status unavailableStatus = Status.UNAVAILABLE.withDescription(
              "NameResolver returned no usable address. addrs=" + resolvedAddresses.getAddresses()
                      + ", attrs=" + resolvedAddresses.getAttributes());
      handleNameResolutionError(unavailableStatus);
      return unavailableStatus;
    } else {
      if (recursionCount++ == 0) {
        handleResolvedAddresses(resolvedAddresses);
      }
      recursionCount = 0;

      return Status.OK;
    }
  }

  /**
   * Handles an error from the name resolution system.
   *
   * @param error a non-OK status
   * @since 1.2.0
   */
  public abstract void handleNameResolutionError(Status error);




}

```

然后创建对应的`Picker`，也就是选择算法。

```java
  /**
   * The main balancing logic.
   * 
   * 必须实现为线程安全的。
   *
   * @since 1.2.0
   */
  @ThreadSafe
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
  public abstract static class SubchannelPicker {
    /**
     * Make a balancing decision for a new RPC.
     *
     * @param args the pick arguments
     * @since 1.3.0
     */
    public abstract PickResult pickSubchannel(PickSubchannelArgs args);

  }
```

最后回调helper的`updateBalancingState`方法。