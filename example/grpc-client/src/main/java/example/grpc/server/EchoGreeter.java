package example.grpc.server;

import examples.grpc.helloworld.GreeterGrpc;
import examples.grpc.helloworld.HelloReply;
import examples.grpc.helloworld.HelloRequest;
import io.grpc.stub.StreamObserver;

public class EchoGreeter extends GreeterGrpc.GreeterImplBase {
    @Override
    public void sayHello(HelloRequest request, StreamObserver<HelloReply> responseObserver) {
        System.out.println("Received message from client: " + request);
        HelloReply reply = HelloReply.newBuilder().setMessage("Echo: " + request.getName()).build();
        responseObserver.onNext(reply);
        responseObserver.onCompleted();
    }
}
