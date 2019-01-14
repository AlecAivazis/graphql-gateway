package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

// QueryPOSTBody is the incoming payload when sending POST requests to the gateway
type QueryPOSTBody struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables"`
	OperationName string                 `json:"operationName"`
}

// GraphQLHandler returns a http.HandlerFunc that should be used as the
// primary endpoint for the gateway API. The endpoint will respond
// to queries on both GET and POST requests.
func (g *Gateway) GraphQLHandler(w http.ResponseWriter, r *http.Request) {
	// a place to store query params
	payload := QueryPOSTBody{}

	var payloadErr error = nil

	// if we got a GET request
	if r.Method == http.MethodGet {
		parameters := r.URL.Query()

		// get the query paramter
		if query, ok := parameters["query"]; ok {
			payload.Query = query[0]

			// include operationName
			if variableInput, ok := parameters["variables"]; ok {
				variables := map[string]interface{}{}

				err := json.Unmarshal([]byte(variableInput[0]), variables)
				if err != nil {
					payloadErr = errors.New("must include query as parameter")
				}

				// assign the variables to the payload
				payload.Variables = variables
			}

			// include operationName
			if operationName, ok := parameters["operationName"]; ok {
				payload.OperationName = operationName[0]
			}
		} else {
			// there was no query parameter
			payloadErr = errors.New("must include query as parameter")
		}
		// or we got a POST request
	} else if r.Method == http.MethodPost {
		// read the full request body
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			payloadErr = fmt.Errorf("encountered error reading body: %s", err.Error())
		}

		err = json.Unmarshal(body, &payload)
		if err != nil {
			payloadErr = fmt.Errorf("encountered error parsing body: %s", err.Error())
		}
	}

	// if there was an error retrieving the payload
	if payloadErr != nil {
		// set the right header
		w.WriteHeader(http.StatusUnprocessableEntity)

		// send the error body back
		fmt.Fprint(w, payloadErr.Error())
		return
	}

	// if we dont have a query
	if payload.Query == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, "Could not find a query in request payload.")
		return
	}

	// fire the query
	result, err := g.Execute(payload.Query)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Encountered error during execution: %s", err.Error())
		return
	}

	response, err := json.Marshal(map[string]interface{}{
		"data": result,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Encountered error marshaling response: %s", err.Error())
		return
	}

	// send the result to the user
	fmt.Fprint(w, string(response))
}

// PlaygroundHandler returns a http.HandlerFunc which on GET requests shows
// the user an interface that they can use to interact with the API. On
// POSTs the endpoint executes the designated query
func (g *Gateway) PlaygroundHandler(w http.ResponseWriter, r *http.Request) {
	// on POSTs, we have to send the request to the graphqlHandler
	if r.Method == http.MethodPost {
		g.GraphQLHandler(w, r)
		return
	}

	// we are not handling a POST request so we have to show the user the playground
	w.Write(playgroundContent)
}
