package gateway

import (
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/alecaivazis/graphql-gateway/graphql"
	"github.com/stretchr/testify/assert"
)

func TestGraphQLHandler_postMissingQuery(t *testing.T) {
	schema, err := graphql.LoadSchema(`
		type Query {
			allUsers: [String!]!
		}
	`)

	// create gateway schema we can test against
	gateway, err := New([]*graphql.RemoteSchema{
		{Schema: schema, URL: "url1"},
	})
	if err != nil {
		t.Error(err.Error())
		return
	}
	// the incoming request
	request := httptest.NewRequest("POST", "/graphql", strings.NewReader(`
		{
			"query": ""
		}
	`))
	// a recorder so we can check what the handler responded with
	responseRecorder := httptest.NewRecorder()

	// call the http hander
	gateway.GraphQLHandler(responseRecorder, request)

	// make sure we got an error code
	assert.Equal(t, http.StatusUnprocessableEntity, responseRecorder.Result().StatusCode)
}

func TestGraphQLHandler(t *testing.T) {
	schema, _ := graphql.LoadSchema(`
		type Query {
			allUsers: [String!]!
		}
	`)

	// create gateway schema we can test against
	gateway, err := New([]*graphql.RemoteSchema{
		{Schema: schema, URL: "url1"},
	}, WithExecutor(&ExecutorFn{
		func(*QueryPlan, map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{
				"Hello": "world",
			}, nil
		},
	}))

	if err != nil {
		t.Error(err.Error())
		return
	}

	t.Run("Missing query", func(t *testing.T) {
		// the incoming request
		request := httptest.NewRequest("GET", "/graphql", strings.NewReader(""))
		// a recorder so we can check what the handler responded with
		responseRecorder := httptest.NewRecorder()

		// call the http hander
		gateway.GraphQLHandler(responseRecorder, request)

		// make sure we got an error code
		assert.Equal(t, http.StatusUnprocessableEntity, responseRecorder.Result().StatusCode)
	})

	t.Run("Non-object variables fails", func(t *testing.T) {
		// the incoming request
		request := httptest.NewRequest("GET", `/graphql?query={allUsers}&variables=true`, strings.NewReader(""))

		// a recorder so we can check what the handler responded with
		responseRecorder := httptest.NewRecorder()
		// call the http hander
		gateway.GraphQLHandler(responseRecorder, request)

		// make sure we got an error code
		assert.Equal(t, http.StatusUnprocessableEntity, responseRecorder.Result().StatusCode)
	})

	t.Run("Object variables succeeds", func(t *testing.T) {
		// the incoming request
		request := httptest.NewRequest("GET", `/graphql?query={allUsers}&variables={"foo":2}`, strings.NewReader(""))
		// a recorder so we can check what the handler responded with
		responseRecorder := httptest.NewRecorder()

		// call the http hander
		gateway.GraphQLHandler(responseRecorder, request)

		// make sure we got an error code
		assert.Equal(t, http.StatusOK, responseRecorder.Result().StatusCode)
	})

	t.Run("OperationName", func(t *testing.T) {
		// the incoming request
		request := httptest.NewRequest("GET", `/graphql?query={allusers}&operationName=Hello`, strings.NewReader(""))
		// a recorder so we can check what the handler responded with
		responseRecorder := httptest.NewRecorder()

		// call the http hander
		gateway.GraphQLHandler(responseRecorder, request)

		// make sure we got an error code
		assert.Equal(t, http.StatusOK, responseRecorder.Result().StatusCode)
	})

	t.Run("error marhsalling response", func(t *testing.T) {
		// create gateway schema we can test against
		innerGateway, err := New([]*graphql.RemoteSchema{
			{Schema: schema, URL: "url1"},
		}, WithExecutor(&ExecutorFn{
			func(*QueryPlan, map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{
					"foo": func() {},
				}, nil
			},
		}))

		if err != nil {
			t.Error(err.Error())
			return
		}

		// the incoming request
		request := httptest.NewRequest("GET", `/graphql?query={allUsers}`, strings.NewReader(""))
		// a recorder so we can check what the handler responded with
		responseRecorder := httptest.NewRecorder()

		// call the http hander
		innerGateway.GraphQLHandler(responseRecorder, request)

		// make sure we got an error code
		assert.Equal(t, http.StatusInternalServerError, responseRecorder.Result().StatusCode)
	})
}

func TestPlaygroundHandler_postRequest(t *testing.T) {
	// a planner that always returns an error
	planner := &MockErrPlanner{Err: errors.New("Planning error")}

	// and some schemas that the gateway wraps
	schema, err := graphql.LoadSchema(`
		type Query {
			allUsers: [String!]!
		}
	`)
	schemas := []*graphql.RemoteSchema{{Schema: schema, URL: "url1"}}

	// create gateway schema we can test against
	gateway, err := New(schemas, WithPlanner(planner))
	if err != nil {
		t.Error(err.Error())
		return
	}
	// the incoming request
	request := httptest.NewRequest("POST", "/graphql", strings.NewReader(`
		{
			"query": "{ allUsers }"
		}
	`))
	// a recorder so we can check what the handler responded with
	responseRecorder := httptest.NewRecorder()

	// call the http hander
	gateway.PlaygroundHandler(responseRecorder, request)

	// get the response from the handler
	response := responseRecorder.Result()
	// read the body
	_, err = ioutil.ReadAll(response.Body)
	if err != nil {
		t.Error(err.Error())
		return
	}

	// make sure we got an error code
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

func TestPlaygroundHandler_getRequest(t *testing.T) {
	// a planner that always returns an error
	planner := &MockErrPlanner{Err: errors.New("Planning error")}

	// and some schemas that the gateway wraps
	schema, err := graphql.LoadSchema(`
		type Query {
			allUsers: [String!]!
		}
	`)
	schemas := []*graphql.RemoteSchema{{Schema: schema, URL: "url1"}}

	// create gateway schema we can test against
	gateway, err := New(schemas, WithPlanner(planner))
	if err != nil {
		t.Error(err.Error())
		return
	}
	// the incoming request
	request := httptest.NewRequest("GET", "/graphql", strings.NewReader(``))
	// a recorder so we can check what the handler responded with
	responseRecorder := httptest.NewRecorder()

	// call the http hander
	gateway.PlaygroundHandler(responseRecorder, request)

	_, err = html.Parse(responseRecorder.Result().Body)
	defer responseRecorder.Result().Body.Close()

	if err != nil {
		t.Error(err.Error())
		return
	}
}
