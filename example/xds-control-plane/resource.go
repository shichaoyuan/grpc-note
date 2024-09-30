package main

import (
	"google.golang.org/protobuf/types/known/anypb"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	ClusterName1 = "greeter-s003"
)

func GenerateSnapshot() *cache.Snapshot {

	routeConfig := route.RouteConfiguration{
		Name: ClusterName1,
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    ClusterName1,
				Domains: []string{"*"},
				Routes: []*route.Route{
					{
						Name: "default",
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{},
						},
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								ClusterSpecifier: &route.RouteAction_Cluster{
									Cluster: ClusterName1,
								},
							},
						},
					},
				},
			},
		},
	}

	router, _ := anypb.New(&router.Router{})
	mgr, _ := anypb.New(&hcm.HttpConnectionManager{
		HttpFilters: []*hcm.HttpFilter{
			{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: router,
				},
			},
		},
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &routeConfig,
		},
	})

	svcListener := listener.Listener{
		Name: ClusterName1,
		ApiListener: &listener.ApiListener{
			ApiListener: mgr,
		},
	}

	svcCluster := cluster.Cluster{
		Name: ClusterName1,
		ClusterDiscoveryType: &cluster.Cluster_Type{
			Type: cluster.Cluster_EDS,
		},
		LbPolicy: cluster.Cluster_ROUND_ROBIN,
		EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
			EdsConfig: &core.ConfigSource{
				ConfigSourceSpecifier: &core.ConfigSource_Ads{
					Ads: &core.AggregatedConfigSource{},
				},
			},
		},
	}

	svcEndpoint := endpoint.ClusterLoadAssignment{
		ClusterName: ClusterName1,
		Endpoints: []*endpoint.LocalityLbEndpoints{
			{
				LoadBalancingWeight: wrapperspb.UInt32(1),
				Locality:            &core.Locality{},
				LbEndpoints: []*endpoint.LbEndpoint{
					{
						HostIdentifier: &endpoint.LbEndpoint_Endpoint{
							Endpoint: &endpoint.Endpoint{
								Address: &core.Address{
									Address: &core.Address_SocketAddress{
										SocketAddress: &core.SocketAddress{
											Protocol: core.SocketAddress_TCP,
											Address:  "127.0.0.1",
											PortSpecifier: &core.SocketAddress_PortValue{
												PortValue: uint32(9991),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	snap, _ := cache.NewSnapshot("1",
		map[resource.Type][]types.Resource{
			resource.ListenerType: {&svcListener},
			resource.ClusterType:  {&svcCluster},
			resource.EndpointType: {&svcEndpoint},
		},
	)
	return snap
}
