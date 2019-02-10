# nautilus/gateway

[![Build Status](https://travis-ci.org/nautilus/gateway.svg?branch=master)](https://travis-ci.org/nautilus/gateway) [![Coverage Status](https://coveralls.io/repos/github/nautilus/gateway/badge.svg?branch=master)](https://coveralls.io/github/nautilus/gateway?branch=master) [![Go Report Card](https://goreportcard.com/badge/github.com/nautilus/gateway)](https://goreportcard.com/report/github.com/nautilus/gateway)

A standalone service designed to consolidate your graphql APIs into one endpoint.

For a guide to getting started read [this post](https://medium.com/@aaivazis/a-guide-to-schema-federation-part-1-995b639ac035)

## Table of Contents

1. [Running the Executable](#running-the-executable)
1. [Customizing the Gateway](#customizing-the-gateway)
   1. [Integrating with an HTTP server](#integrating-with-an-http-server)
   1. [Modifying Service Requests](#modifying-service-requests)
   1. [Authentication](#authentication-and-authorization)
1. [Versioning](#versioning)
1. [Examples](./examples)
   1. [Hello World](./examples/hello)
   1. [Authentication and Authorization](./examples/auth)

## Running the Executable

The simplest way to run a gateway is to download the executable
from the latest release on GitHub and then run it directly on
your machine:

```bash
$ ./gateway start --port 4000 --services http://localhost:3000,http://localhost:3001
```

This will start a server on port 4000 that wraps over the services
running at `http://localhost:3000` and `http://localhost:3001`. For more information on possible
arguments to pass the executable, run `./gateway --help`.

## Customizing the Gateway

While the executable is good for getting started quickly, it isn't sufficient for
most production usecases. Unfortunately, there is currently no story to plug in custom
authentication/authorization logic or other extensions when running a gateway with the
cli. If being able to run you're own gateway with the cli is something that interests you,
please open in an issue! For now, these common situations require building your own executable.

The core object of the gateway is the `Gateway` struct exported by the module at
`github.com/nautilus/gateway`. A `Gateway` is constructed by providing
a list of `graphql.RemoteSchema`s for each service to `gateway.New`. The easiest way to
get a `graphql.RemoteSchema` is to introspect the remote schema using a utility from
`github.com/nautilus/graphql`:

```golang
package main

import (
	"github.com/nautilus/gateway"
	"github.com/nautilus/graphql"
)

func main() {
	// introspect the apis
	schemas, err := graphql.IntrospectRemoteSchemas(
		"http://localhost:3000",
		"http://localhost:3001",
    	)
	if err != nil {
		panic(err)
	}

	// create the gateway instance
	gw, err := gateway.New(schemas)
	if err != nil {
		panic(err)
	}
}
```

### Integrating with an HTTP server

An instance of `Gateway` provides 2 different handlers which are both instances of `http.HandlerFunc`:

- `gw.GraphQLHandler` responds to both `GET` and `POST` requests as described
  [in the spec](https://graphql.org/learn/serving-over-http/).
- `gw.PlaygroundHandler` responds to `GET` requests with a web-based IDE for easy exploration
  and interprets `POST`s as queries to process.

```golang
package main

import (
	"fmt"
	"net/http"

	// ... including the ones above
)

func main() {
	// ... including up above

	// add the playground endpoint to the router
	http.HandleFunc("/graphql", gw.PlaygroundHandler)

	// start the server
	fmt.Println("Starting server")
	err = http.ListenAndServe(":3001", nil)
	if err != nil {
		fmt.Println(err.Error())
	}
}
```

### Modifying Service Requests

There are many situations where one might want to modify the network requests sent from
the gateway to the other services. In order to do this, you can define a `RequestMiddleware`
that will be called for every request sent. The context of this request is the same context
of the incoming network request.

```golang
addHeader := gateway.RequestMiddleware(func(r *http.Request) error {
	r.Header.Set("AwesomeHeader", "MyValue")

	// return the modified request
	return nil
})

// ... somewhere else ...

gateway.New(..., gateway.WithMiddlewares(addHeader))
```

#### Forwarding headers

If you wanted to do something more complicated like pull something out of the incoming
network request (its IP for example) and add it to the outbound requests, you would
write it in 2 parts.

The first would grab the the value from the incoming request and set it in context

```golang
func withIP(handler http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// invoke the wrapped handler with the new context
		handler.ServeHTTP(w, r.WithContext(
			context.WithValue(r.Context(), "source-ip", r.RemoteAddr),
		))
	})
}
```

Then, you would define a middleware similar to above that takes the value out of context
and sets a header in the outbound requests:

```golang
addHeader := gateway.RequestMiddleware(func(r *http.Request) error {
	// i know i know ... context.Value is the worst. Feel free to put your favorite workaround here
	r.Header.Set("X-Forwarded-For", r.Context().Value("source-ip").(string))

	// no errors
	return nil
})

// ... somewhere else ...

gateway.New(..., gateway.WithMiddlewares(addHeader))
```

#### Authentication and Authorization

Currently the gateway has no opinion on a method for authentication and authorization.
Descisions for wether a user can or cannot do something is pushed down to the services
handling the queries. Authentication and Authorization should be modeled as a special
case of above.

See the [auth example](./examples/auth) for more information.

### Modifying the response

There are some situations (error logging for example) that require hooking into the
execution process after a response has been generated. To do this, define a
`gateway.ResponseMiddleware` and pass it to the gateway constructor:

```golang

logResponse := gateway.ResponseMiddleware(func(ctx *gateway.ExecutionContext, response map[string]interface{}) error {
	// you can also modify the response directly if you wanted
	fmt.Println(response)

	return nil
})

// ... somewhere else ...

gateway.New(..., gateway.WithMiddlewares(logResponse))

```

### Gateway fields

There are some situations where it makes sense to have fields that resolve at the gateway layer.
One common example is the `viewer` field that resolves to the `User` type representing the
current user. This can be achieved by adding a custom field to the gateway whose resolver returns
the id of the entity.

```golang
import (
	// ...
	"github.com/vektah/gqlparser/ast"
)

viewerField := &gateway.QueryField{
	Name: "viewer",
	Type: ast.NamedType("User", &ast.Position{}),
	Resolver: func(ctx context.Context, args ast.ArgumentList, variables map[string]interface{}) (string, error) {
		// for now just return the value in context
		return context.value("user-id").(string), nil
	},
}

// ... somewhere else ...

gateway.New(..., gateway.withFields(viewerField))
```

#### Field Arguments

Grabbing the value of a particular argument should be done through a method on the second argument:

```golang
// this field is equivalent to
// 		myField(myArg: String!): User
argField := &gateway.QueryField{
	Name: "myField",
	Type: ast.NamedType("User", &ast.Position{}),
	Arguments: ast.ArgumentDefinitionList{
		&ast.ArgumentDefinition{
			Name: "id",
			Type: ast.NonNullNamedType("ID", &ast.Position{}),
		},
	},
	Resolver: func(ctx context.Context, args map[string]interface{}) (string, error) {
		// we know its a string
		return args["myArg"].(string), nil
	},
}
```

## Versioning

This project is built as a go module and follows the practices outlined in the [spec](https://github.com/golang/go/wiki/Modules). Please consider all APIs experimental and subject
to change until v1 has been released at which point semantic versioning will be strictly followed.
