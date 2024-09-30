package example.grpc.client;

import examples.grpc.helloworld.GreeterGrpc;
import examples.grpc.helloworld.HelloReply;
import examples.grpc.helloworld.HelloRequest;
import io.grpc.Grpc;
import io.grpc.InsecureChannelCredentials;
import io.grpc.ManagedChannel;
import io.grpc.NameResolverRegistry;
import io.grpc.xds.XdsNameResolverProvider;

public class Client {
    public static void main(String[] args) {
        System.setProperty("io.grpc.xds.bootstrap", "./bootstrap.json");

        NameResolverRegistry.getDefaultRegistry().register(new XdsNameResolverProvider());

        ManagedChannel channel = Grpc.newChannelBuilder("xds:///greeter-s003", InsecureChannelCredentials.create())
                .build();

        GreeterGrpc.GreeterBlockingStub stub = GreeterGrpc.newBlockingStub(channel);

        HelloRequest req = HelloRequest.newBuilder().setName("xds").build();
        HelloReply reply = stub.sayHello(req);
        System.out.println(reply);
    }
}