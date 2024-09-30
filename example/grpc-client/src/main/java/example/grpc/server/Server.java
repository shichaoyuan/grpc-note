package example.grpc.server;

import io.grpc.ServerBuilder;

import java.io.IOException;
import java.util.concurrent.TimeUnit;

public class Server {
    public static void main(String[] args) throws IOException, InterruptedException {

        io.grpc.Server server = ServerBuilder.forPort(9991)
                .addService(new EchoGreeter())
                .build();
        server.start();

        Runtime.getRuntime().addShutdownHook(new Thread() {
            @Override
            public void run() {
                server.shutdown();
                try {
                    if (!server.awaitTermination(30, TimeUnit.SECONDS)) {
                        server.shutdownNow();
                        server.awaitTermination(5, TimeUnit.SECONDS);
                    }
                } catch (InterruptedException ex) {
                    server.shutdownNow();
                }
            }
        });

        server.awaitTermination();




    }
}
