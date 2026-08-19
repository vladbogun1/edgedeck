package version

// Version подставляется линкером в CI:
//
//	-ldflags "-X github.com/vladbogun1/edgedeck/internal/version.Version=sha-abc1234"
var Version = "dev"
