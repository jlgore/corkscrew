module github.com/jlgore/corkscrew/plugins/github-provider

go 1.26.2

require (
	github.com/bradleyfalzon/ghinstallation/v2 v2.15.0
	github.com/google/go-github/v71 v71.0.0
	github.com/hashicorp/go-plugin v1.8.0
	github.com/jlgore/corkscrew v0.1.2
	github.com/shurcooL/githubv4 v0.0.0-20260209031235-2402fdf4a9ed
	golang.org/x/oauth2 v0.36.0
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
)

require (
	github.com/fatih/color v1.19.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/oklog/run v1.2.0 // indirect
	github.com/shurcooL/graphql v0.0.0-20240915155400-7ee5256398cf // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260504160031-60b97b32f348 // indirect
	google.golang.org/grpc v1.81.0 // indirect
)

replace github.com/jlgore/corkscrew => ../..
