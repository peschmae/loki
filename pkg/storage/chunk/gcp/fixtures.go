package gcp

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/bigtable"
	"cloud.google.com/go/bigtable/bttest"
	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/objectclient"
	"github.com/grafana/loki/pkg/storage/chunk/testutils"
)

const (
	proj, instance = "proj", "instance"
)

type fixture struct {
	btsrv  *bttest.Server
	gcssrv *fakestorage.Server

	name string

	gcsObjectClient bool
	columnKeyClient bool
	hashPrefix      bool
}

func (f *fixture) Name() string {
	return f.name
}

func (f *fixture) Clients() (
	iClient chunk.IndexClient, cClient chunk.Client, tClient chunk.TableClient,
	schemaConfig chunk.SchemaConfig, closer io.Closer, err error,
) {
	f.btsrv, err = bttest.NewServer("localhost:0")
	if err != nil {
		return
	}

	f.gcssrv = fakestorage.NewServer(nil)
	f.gcssrv.CreateBucket("chunks")

	conn, err := grpc.Dial(f.btsrv.Addr, grpc.WithInsecure())
	if err != nil {
		return
	}

	ctx := context.Background()
	adminClient, err := bigtable.NewAdminClient(ctx, proj, instance, option.WithGRPCConn(conn))
	if err != nil {
		return
	}

	schemaConfig = testutils.DefaultSchemaConfig("gcp-columnkey")
	tClient = &tableClient{
		client: adminClient,
	}

	client, err := bigtable.NewClient(ctx, proj, instance, option.WithGRPCConn(conn))
	if err != nil {
		return
	}

	cfg := Config{
		DistributeKeys: f.hashPrefix,
	}
	if f.columnKeyClient {
		iClient = newStorageClientColumnKey(cfg, schemaConfig, client)
	} else {
		iClient = newStorageClientV1(cfg, schemaConfig, client)
	}

	if f.gcsObjectClient {
		var c *GCSObjectClient
		c, err = newGCSObjectClient(ctx, GCSConfig{BucketName: "chunks"}, hedging.Config{}, func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
			return f.gcssrv.Client(), nil
		})
		if err != nil {
			return
		}
		cClient = objectclient.NewClient(c, nil)
	} else {
		cClient = newBigtableObjectClient(Config{}, schemaConfig, client)
	}

	closer = testutils.CloserFunc(func() error {
		conn.Close()
		return nil
	})

	return
}

// Fixtures for unit testing GCP storage.
var Fixtures = func() []testutils.Fixture {
	fixtures := []testutils.Fixture{}
	for _, gcsObjectClient := range []bool{true, false} {
		for _, columnKeyClient := range []bool{true, false} {
			for _, hashPrefix := range []bool{true, false} {
				fixtures = append(fixtures, &fixture{
					name:            fmt.Sprintf("bigtable-columnkey:%v-gcsObjectClient:%v-hashPrefix:%v", columnKeyClient, gcsObjectClient, hashPrefix),
					columnKeyClient: columnKeyClient,
					gcsObjectClient: gcsObjectClient,
					hashPrefix:      hashPrefix,
				})
			}
		}
	}
	return fixtures
}()
