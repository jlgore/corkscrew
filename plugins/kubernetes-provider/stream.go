package main

import (
	"context"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// streamBufferSize bounds how far a stream producer may run ahead of the gRPC sender.
const streamBufferSize = 100

// resourceStreamer emits resources on resourceChan and closes it before it returns.
// The producer is the only party that may close resourceChan.
type resourceStreamer interface {
	StreamScanResources(ctx context.Context, names []string, resourceChan chan<- *pb.Resource) error
}

// streamResources runs streamer in its own goroutine and passes every resource it
// emits to send. It only receives, because the producer owns the channel. It waits
// for the producer to return before returning itself, so a scan error can never lose
// a race against the channel closing.
func streamResources(
	parent context.Context,
	streamer resourceStreamer,
	names []string,
	send func(*pb.Resource) error,
) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	resourceChan := make(chan *pb.Resource, streamBufferSize)
	scanErr := make(chan error, 1)
	go func() {
		scanErr <- streamer.StreamScanResources(ctx, names, resourceChan)
	}()

	sendErr := forwardResources(resourceChan, send)
	if sendErr != nil {
		// Stop the producer, then drain so it never blocks on a full channel. The
		// drain ends when the producer observes the cancel and closes the channel.
		cancel()
		for range resourceChan {
		}
	}

	err := <-scanErr
	if sendErr != nil {
		return sendErr
	}
	if err != nil {
		return err
	}
	// A producer may skip work on cancellation without reporting it, so a cancelled
	// caller must not see a clean result.
	return parent.Err()
}

// forwardResources receives until resourceChan closes or send fails.
func forwardResources(resourceChan <-chan *pb.Resource, send func(*pb.Resource) error) error {
	for resource := range resourceChan {
		if err := send(resource); err != nil {
			return err
		}
	}
	return nil
}
