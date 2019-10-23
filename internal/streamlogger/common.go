/*

Package streamlogger creates some stream client and server which can proxy logs to client, by adding to the grpc
call some metadata used to identify and specify log levels.

*/
package streamlogger

const (
	// DefaultSocket path.
	DefaultSocket = "/run/zsysd.sock"

	// metaRequesterIDKey is the metadata key provided by the client to associate a given requester
	metaRequesterIDKey = "requesterid"
	// metaLevelKey is the metadata key provided by the client to request a particular logging level
	metaLevelKey = "loglevel"
	// metaRequestIDKey is the metadata key used to associate a given request (set by the service).
	metaRequestIDKey = "requestid"
)
