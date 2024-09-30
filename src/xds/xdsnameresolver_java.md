# XdsNameResolver(Java)

Java版本的grpc-xds使用的是ADS，资源的调用顺序与[envoy文档](https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#eventual-consistency-considerations)略有区别。

[整体类图](./images/xdsnameresolver.svg)

## Bootstrapper

按照

在`ManagedChannel`中`NameResolver`的起始方法是start，所以首先从`XdsNameResolver`的start方法开始。










