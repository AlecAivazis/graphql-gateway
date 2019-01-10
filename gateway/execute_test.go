package gateway

import (
	"testing"

	"github.com/alecaivazis/graphql-gateway/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/ast"
)

func TestExecutor_plansOfOne(t *testing.T) {
	// build a query plan that the executor will follow
	result, err := (&ParallelExecutor{}).Execute(&QueryPlan{
		RootStep: &QueryPlanStep{
			Then: []*QueryPlanStep{
				{
					// this is equivalent to
					// query { values }
					ParentType: "Query",
					SelectionSet: ast.SelectionSet{
						&ast.Field{
							Name: "values",
							Definition: &ast.FieldDefinition{
								Type: ast.ListType(ast.NamedType("String", &ast.Position{}), &ast.Position{}),
							},
						},
					},
					// return a known value we can test against
					Queryer: &graphql.MockQueryer{map[string]interface{}{
						"values": []string{
							"hello",
							"world",
						},
					}},
				},
			},
		},
	})
	if err != nil {
		t.Errorf("Encountered error executing plan: %v", err.Error())
	}

	// get the result back
	values, ok := result["values"]
	if !ok {
		t.Errorf("Did not get any values back from the execution")
	}

	// make sure we got the right values back
	assert.Equal(t, []string{"hello", "world"}, values)
}

func TestExecutor_plansWithDependencies(t *testing.T) {
	// the query we want to execute is
	// {
	// 		user {                   <- from serviceA
	//      	firstName            <- from serviceA
	// 			favoriteCatPhoto {   <- from serviceB
	// 				url              <- from serviceB
	// 			}
	// 		}
	// }

	// build a query plan that the executor will follow
	result, err := (&ParallelExecutor{}).Execute(&QueryPlan{
		RootStep: &QueryPlanStep{
			Then: []*QueryPlanStep{
				{

					// this is equivalent to
					// query { user }
					ParentType:     "Query",
					InsertionPoint: []string{},
					SelectionSet: ast.SelectionSet{
						&ast.Field{
							Name: "user",
							Definition: &ast.FieldDefinition{
								Type: ast.NamedType("User", &ast.Position{}),
							},
							SelectionSet: ast.SelectionSet{
								&ast.Field{
									Name: "firstName",
									Definition: &ast.FieldDefinition{
										Type: ast.NamedType("String", &ast.Position{}),
									},
								},
							},
						},
					},
					// return a known value we can test against
					Queryer: &graphql.MockQueryer{map[string]interface{}{
						"user": map[string]interface{}{
							"id":        "1",
							"firstName": "hello",
						},
					}},
					// then we have to ask for the users favorite cat photo and its url
					Then: []*QueryPlanStep{
						{
							ParentType:     "User",
							InsertionPoint: []string{"user", "favoriteCatPhoto"},
							SelectionSet: ast.SelectionSet{
								&ast.Field{
									Name: "favoriteCatPhoto",
									Definition: &ast.FieldDefinition{
										Type: ast.NamedType("User", &ast.Position{}),
									},
									SelectionSet: ast.SelectionSet{
										&ast.Field{
											Name: "url",
											Definition: &ast.FieldDefinition{
												Type: ast.NamedType("String", &ast.Position{}),
											},
										},
									},
								},
							},
							Queryer: &graphql.MockQueryer{map[string]interface{}{
								"node": map[string]interface{}{
									"favoriteCatPhoto": map[string]interface{}{
										"url": "hello world",
									},
								},
							}},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Errorf("Encountered error executing plan: %v", err.Error())
		return
	}

	// make sure we got the right values back
	assert.Equal(t, map[string]interface{}{
		"user": map[string]interface{}{
			"id":        "1",
			"firstName": "hello",
			"favoriteCatPhoto": map[string]interface{}{
				"url": "hello world",
			},
		},
	}, result)
}

func TestExecutor_emptyPlansWithDependencies(t *testing.T) {
	// the query we want to execute is
	// {
	// 		user {                   <- from serviceA
	//      	firstName            <- from serviceA
	// 		}
	// }

	// build a query plan that the executor will follow
	result, err := (&ParallelExecutor{}).Execute(&QueryPlan{
		RootStep: &QueryPlanStep{
			Then: []*QueryPlanStep{
				{ // this is equivalent to
					// query { user }
					ParentType:     "Query",
					InsertionPoint: []string{},
					// return a known value we can test against
					Queryer: &graphql.MockQueryer{map[string]interface{}{
						"user": map[string]interface{}{
							"id":        "1",
							"firstName": "hello",
						},
					}},
					// then we have to ask for the users favorite cat photo and its url
					Then: []*QueryPlanStep{
						{
							ParentType:     "Query",
							InsertionPoint: []string{},
							SelectionSet: ast.SelectionSet{
								&ast.Field{
									Name: "user",
									Definition: &ast.FieldDefinition{
										Type: ast.NamedType("User", &ast.Position{}),
									},
									SelectionSet: ast.SelectionSet{
										&ast.Field{
											Name: "firstName",
											Definition: &ast.FieldDefinition{
												Type: ast.NamedType("String", &ast.Position{}),
											},
										},
									},
								},
							},
							// return a known value we can test against
							Queryer: &graphql.MockQueryer{map[string]interface{}{
								"user": map[string]interface{}{
									"id":        "1",
									"firstName": "hello",
								},
							}},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Errorf("Encountered error executing plan: %v", err.Error())
		return
	}

	// make sure we got the right values back
	assert.Equal(t, map[string]interface{}{
		"user": map[string]interface{}{
			"id":        "1",
			"firstName": "hello",
		},
	}, result)
}

func TestExecutor_insertIntoLists(t *testing.T) {
	// the query we want to execute is
	// {
	// 		users {                  	<- Query.services @ serviceA
	//      	firstName
	//          friends {
	//              firstName
	//              photoGallery {   	<- User.photoGallery @ serviceB
	// 			    	url
	// 					followers {
	//                  	firstName	<- User.firstName @ serviceA
	//                  }
	// 			    }
	//          }
	// 		}
	// }

	// values to test against
	photoGalleryURL := "photoGalleryURL"
	followerName := "John"

	// build a query plan that the executor will follow
	result, err := (&ParallelExecutor{}).Execute(&QueryPlan{
		RootStep: &QueryPlanStep{
			Then: []*QueryPlanStep{
				{
					ParentType:     "Query",
					InsertionPoint: []string{},
					SelectionSet: ast.SelectionSet{
						&ast.Field{
							Name: "users",
							Definition: &ast.FieldDefinition{
								Type: ast.ListType(ast.NamedType("User", &ast.Position{}), &ast.Position{}),
							},
							SelectionSet: ast.SelectionSet{
								&ast.Field{
									Name: "firstName",
									Definition: &ast.FieldDefinition{
										Type: ast.NamedType("String", &ast.Position{}),
									},
								},
								&ast.Field{
									Name: "friends",
									Definition: &ast.FieldDefinition{
										Type: ast.ListType(ast.NamedType("User", &ast.Position{}), &ast.Position{}),
									},
									SelectionSet: ast.SelectionSet{
										&ast.Field{
											Definition: &ast.FieldDefinition{
												Type: ast.NamedType("String", &ast.Position{}),
											},
											Name: "firstName",
										},
									},
								},
							},
						},
					},
					// planner will actually leave behind a queryer that hits service A
					// for testing we can just return a known value
					Queryer: &graphql.MockQueryer{map[string]interface{}{
						"users": []interface{}{
							map[string]interface{}{
								"firstName": "hello",
								"friends": []interface{}{
									map[string]interface{}{
										"firstName": "John",
										"id":        "1",
									},
									map[string]interface{}{
										"firstName": "Jacob",
										"id":        "2",
									},
								},
							},
							map[string]interface{}{
								"firstName": "goodbye",
								"friends": []interface{}{
									map[string]interface{}{
										"firstName": "Jingleheymer",
										"id":        "1",
									},
									map[string]interface{}{
										"firstName": "Schmidt",
										"id":        "2",
									},
								},
							},
						},
					}},
					// then we have to ask for the users photo gallery
					Then: []*QueryPlanStep{
						// a query to satisfy User.photoGallery
						{
							ParentType:     "User",
							InsertionPoint: []string{"users", "friends", "photoGallery"},
							SelectionSet: ast.SelectionSet{
								&ast.Field{
									Name: "photoGallery",
									Definition: &ast.FieldDefinition{
										Type: ast.ListType(ast.NamedType("CatPhoto", &ast.Position{}), &ast.Position{}),
									},
									SelectionSet: ast.SelectionSet{
										&ast.Field{
											Name: "url",
											Definition: &ast.FieldDefinition{
												Type: ast.NamedType("String", &ast.Position{}),
											},
										},
										&ast.Field{
											Name: "followers",
											Definition: &ast.FieldDefinition{
												Type: ast.NamedType("User", &ast.Position{}),
											},
											SelectionSet: ast.SelectionSet{},
										},
									},
								},
							},
							// planner will actually leave behind a queryer that hits service B
							// for testing we can just return a known value
							Queryer: &graphql.MockQueryer{map[string]interface{}{
								"node": map[string]interface{}{
									"photoGallery": []interface{}{
										map[string]interface{}{
											"url": photoGalleryURL,
											"followers": []interface{}{
												map[string]interface{}{
													"id": "1",
												},
											},
										},
									},
								},
							}},
							Then: []*QueryPlanStep{
								// a query to satisfy User.firstName
								{
									ParentType:     "User",
									InsertionPoint: []string{"users", "friends", "photoGallery", "followers", "firstName"},
									SelectionSet: ast.SelectionSet{
										&ast.Field{
											Name: "firstName",
											Definition: &ast.FieldDefinition{
												Type: ast.NamedType("String", &ast.Position{}),
											},
										},
									},
									// planner will actually leave behind a queryer that hits service B
									// for testing we can just return a known value
									Queryer: &graphql.MockQueryer{map[string]interface{}{
										"node": map[string]interface{}{
											"firstName": followerName,
										},
									}},
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Errorf("Encountered error executing plan: %v", err.Error())
		return
	}

	// atm the mock queryer always returns the same value so we will end up with
	// the same User.favoritePhoto and User.photoGallery
	assert.Equal(t, map[string]interface{}{
		"users": []map[string]interface{}{
			{
				"firstName": "hello",
				"friends": []map[string]interface{}{
					{
						"firstName": "John",
						"id":        "1",
						"photoGallery": []map[string]interface{}{
							{
								"url": photoGalleryURL,
								"followers": []map[string]interface{}{
									{
										"id":        "1",
										"firstName": followerName,
									},
								},
							},
						},
					},
					{
						"firstName": "Jacob",
						"id":        "2",
						"photoGallery": []map[string]interface{}{
							{
								"url": photoGalleryURL,
								"followers": []map[string]interface{}{
									{
										"id":        "1",
										"firstName": followerName,
									},
								},
							},
						},
					},
				},
			},
			{
				"firstName": "goodbye",
				"friends": []map[string]interface{}{
					{
						"firstName": "Jingleheymer",
						"id":        "1",
						"photoGallery": []map[string]interface{}{
							{
								"url": photoGalleryURL,
								"followers": []map[string]interface{}{
									{
										"id":        "1",
										"firstName": followerName,
									},
								},
							},
						},
					},
					{
						"firstName": "Schmidt",
						"id":        "2",
						"photoGallery": []map[string]interface{}{
							{
								"url": photoGalleryURL,
								"followers": []map[string]interface{}{
									{
										"id":        "1",
										"firstName": followerName,
									},
								},
							},
						},
					},
				},
			},
		},
	}, result)
}

func TestFindInsertionPoint_rootList(t *testing.T) {
	// in this example, the step before would have just resolved (need to be inserted at)
	// ["users", "photoGallery"]. There would be an id field underneath each photo in the list
	// of users.photoGallery

	// we want the list of insertion points that point to
	planInsertionPoint := []string{"users", "photoGallery", "likedBy", "firstName"}

	// pretend we are in the middle of stitching a larger object
	startingPoint := [][]string{}

	// there are 6 total insertion points in this example
	finalInsertionPoint := [][]string{
		// photo 0 is liked by 2 users whose firstName we have to resolve
		{"users:0", "photoGallery:0", "likedBy:0#1", "firstName"},
		{"users:0", "photoGallery:0", "likedBy:1#2", "firstName"},
		// photo 1 is liked by 3 users whose firstName we have to resolve
		{"users:0", "photoGallery:1", "likedBy:0#3", "firstName"},
		{"users:0", "photoGallery:1", "likedBy:1#4", "firstName"},
		{"users:0", "photoGallery:1", "likedBy:2#5", "firstName"},
		// photo 2 is liked by 1 user whose firstName we have to resolve
		{"users:0", "photoGallery:2", "likedBy:0#6", "firstName"},
	}

	// the selection we're going to make
	stepSelectionSet := ast.SelectionSet{
		&ast.Field{
			Name: "users",
			Definition: &ast.FieldDefinition{
				Type: ast.ListType(ast.NamedType("User", &ast.Position{}), &ast.Position{}),
			},
			SelectionSet: ast.SelectionSet{
				&ast.Field{
					Name: "photoGallery",
					Definition: &ast.FieldDefinition{
						Type: ast.ListType(ast.NamedType("Photo", &ast.Position{}), &ast.Position{}),
					},
					SelectionSet: ast.SelectionSet{
						&ast.Field{
							Name: "likedBy",
							Definition: &ast.FieldDefinition{
								Type: ast.ListType(ast.NamedType("User", &ast.Position{}), &ast.Position{}),
							},
							SelectionSet: ast.SelectionSet{
								&ast.Field{
									Name: "totalLikes",
									Definition: &ast.FieldDefinition{
										Type: ast.NamedType("Int", &ast.Position{}),
									},
								},
								&ast.Field{
									Name: "id",
									Definition: &ast.FieldDefinition{
										Type: ast.NamedType("ID", &ast.Position{}),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// the result of the step
	result := map[string]interface{}{
		"users": []interface{}{
			map[string]interface{}{
				"photoGallery": []interface{}{
					map[string]interface{}{
						"likedBy": []interface{}{
							map[string]interface{}{
								"totalLikes": 10,
								"id":         "1",
							},
							map[string]interface{}{
								"totalLikes": 10,
								"id":         "2",
							},
						},
					},
					map[string]interface{}{
						"likedBy": []interface{}{
							map[string]interface{}{
								"totalLikes": 10,
								"id":         "3",
							},
							map[string]interface{}{
								"totalLikes": 10,
								"id":         "4",
							},
							map[string]interface{}{
								"totalLikes": 10,
								"id":         "5",
							},
						},
					},
					map[string]interface{}{
						"likedBy": []interface{}{
							map[string]interface{}{
								"totalLikes": 10,
								"id":         "6",
							},
						},
					},
					map[string]interface{}{
						"likedBy": []interface{}{},
					},
				},
			},
		},
	}

	generatedPoint, err := findInsertionPoints(planInsertionPoint, stepSelectionSet, result, startingPoint, false)
	if err != nil {
		t.Error(t, err)
		return
	}

	assert.Equal(t, finalInsertionPoint, generatedPoint)
}

func TestFindObject(t *testing.T) {
	// create an object we want to extract
	source := map[string]interface{}{
		"hello": []interface{}{
			map[string]interface{}{
				"firstName": "0",
				"friends": []interface{}{
					map[string]interface{}{
						"firstName": "2",
					},
					map[string]interface{}{
						"firstName": "3",
					},
				},
			},
			map[string]interface{}{
				"firstName": "4",
				"friends": []interface{}{
					map[string]interface{}{
						"firstName": "5",
					},
					map[string]interface{}{
						"firstName": "6",
					},
				},
			},
		},
	}

	value, err := executorExtractValue(source, []string{"hello:0", "friends:1"})
	if err != nil {
		t.Error(err.Error())
		return
	}

	assert.Equal(t, map[string]interface{}{
		"firstName": "3",
	}, value)
}

func TestFindString(t *testing.T) {
	// create an object we want to extract
	source := map[string]interface{}{
		"hello": []interface{}{
			map[string]interface{}{
				"firstName": "0",
				"friends": []interface{}{
					map[string]interface{}{
						"firstName": "2",
					},
					map[string]interface{}{
						"firstName": "3",
					},
				},
			},
			map[string]interface{}{
				"firstName": "4",
				"friends": []interface{}{
					map[string]interface{}{
						"firstName": "5",
					},
					map[string]interface{}{
						"firstName": "6",
					},
				},
			},
		},
	}

	value, err := executorExtractValue(source, []string{"hello:0", "friends:1", "firstName"})
	if err != nil {
		t.Error(err.Error())
		return
	}

	assert.Equal(t, "3", value)
}

func TestExecutorInsertObject_insertValue(t *testing.T) {
	// the object to mutate
	source := map[string]interface{}{}

	// the object to insert
	inserted := "world"

	// insert the string deeeeep down
	err := executorInsertObject(source, []string{"hello:5#1", "message", "body:2", "hello"}, inserted)
	if err != nil {
		t.Error(err)
		return
	}

	// there should be a list under the key "hello"
	rootList, ok := source["hello"]
	if !ok {
		t.Error("Did not add root list")
		return
	}
	list, ok := rootList.([]interface{})
	if !ok {
		t.Error("root list is not a list")
		return
	}

	if len(list) != 6 {
		t.Errorf("Root list did not have enough entries.")
		assert.Equal(t, 6, len(list))
		return
	}

	entry, ok := list[5].(map[string]interface{})
	if !ok {
		t.Error("6th entry wasn't an object")
		return
	}

	// the object we care about is index 5
	message := entry["message"]
	if message == nil {
		t.Error("Did not add message to object")
		return
	}

	msgObj, ok := message.(map[string]interface{})
	if !ok {
		t.Error("message is not a list")
		return
	}

	// there should be a list under it called body
	bodiesList, ok := msgObj["body"]
	if !ok {
		t.Error("Did not add body list")
		return
	}
	bodies, ok := bodiesList.([]interface{})
	if !ok {
		t.Error("bodies list is not a list")
		return
	}

	if len(bodies) != 3 {
		t.Error("bodies list did not have enough entries")
		return
	}
	body, ok := bodies[2].(map[string]interface{})
	if !ok {
		t.Error("Body was not an object")
		return
	}

	// make sure that the value is what we expect
	assert.Equal(t, inserted, body["hello"])
}

func TestExecutorInsertObject_insertListElements(t *testing.T) {
	// the object to mutate
	source := map[string]interface{}{}

	// the object to insert
	inserted := map[string]interface{}{
		"hello": "world",
	}

	// insert the object deeeeep down
	err := executorInsertObject(source, []string{"hello", "objects:5"}, inserted)
	if err != nil {
		t.Error(err)
		return
	}

	// there should be an object under the key "hello"
	rootEntry, ok := source["hello"]
	if !ok {
		t.Error("Did not add root entry")
		return
	}

	root, ok := rootEntry.(map[string]interface{})
	if !ok {
		t.Error("root object is not an object")
		return
	}

	rootList, ok := root["objects"]
	if !ok {
		t.Error("did not add objects list")
		return
	}

	list, ok := rootList.([]map[string]interface{})
	if !ok {
		t.Error("objects is not a list")
		return
	}

	if len(list) != 6 {
		t.Errorf("Root list did not have enough entries.")
		assert.Equal(t, 6, len(list))
		return
	}

	// make sure that the value is what we expect
	assert.Equal(t, inserted, list[5])
}

func TestExecutorBuildQuery_query(t *testing.T) {
	// if we pass a query on Query to the builder we should get that same
	// selection set present in the operation without any nesting
	selection := ast.SelectionSet{
		&ast.Field{
			Name: "allUsers",
			Definition: &ast.FieldDefinition{
				Type: ast.ListType(ast.NamedType("User", &ast.Position{}), &ast.Position{}),
			},
			SelectionSet: ast.SelectionSet{
				&ast.Field{
					Name: "firstName",
				},
			},
		},
	}

	// the query we're building goes to the top level Query object
	operation := executorBuildQuery("Query", "", selection)
	if operation == nil {
		t.Error("Did not receive a query.")
		return
	}

	// it should be a query
	assert.Equal(t, ast.Query, operation.Operation)

	// the selection set should be the same as what we passed in
	assert.Equal(t, selection, operation.SelectionSet)
}

func TestExecutorBuildQuery_node(t *testing.T) {
	// if we are querying a specific type/id then we need to perform a query similar to
	// {
	// 		node(id: "1234") {
	// 			... on User {
	// 				firstName
	// 			}
	// 		}
	// }

	// the type we are querying
	objType := "User"
	// the id of the object
	objID := "1234"

	// we only need the first name for this query
	selection := ast.SelectionSet{
		&ast.Field{
			Name: "firstName",
			Definition: &ast.FieldDefinition{
				Type: ast.NamedType("String", &ast.Position{}),
			},
		},
	}

	// the query we're building goes to the User object
	operation := executorBuildQuery(objType, objID, selection)
	if operation == nil {
		t.Error("Did not receive a query.")
		return
	}

	// it should be a query
	assert.Equal(t, ast.Query, operation.Operation)

	// there should be one selection (node) with an argument for the id
	if len(operation.SelectionSet) != 1 {
		t.Error("Did not find the right number of fields on the top query")
		return
	}

	// grab the node field
	node, ok := operation.SelectionSet[0].(*ast.Field)
	if !ok {
		t.Error("root is not a field")
		return
	}
	if node.Name != "node" {
		t.Error("Did not ask for node at the top")
		return
	}
	// there should be one argument (id)
	if len(node.Arguments) != 1 {
		t.Error("Found the wrong number of arguments for the node field")
		return
	}
	argument := node.Arguments[0]
	if argument.Name != "id" {
		t.Error("Did not pass id to the node field")
		return
	}
	if argument.Value.Raw != objID {
		t.Error("Did not pass the right id value to the node field")
		return
	}
	if argument.Value.Kind != ast.StringValue {
		t.Error("Argument was incorrect type")
		return
	}

	// make sure the field has an inline fragment for the type
	if len(node.SelectionSet) != 1 {
		t.Error("Did not have any sub selection of the node field")
		return
	}
	fragment, ok := node.SelectionSet[0].(*ast.InlineFragment)
	if !ok {
		t.Error("Could not find inline fragment under node")
		return
	}

	// make sure its for the right type
	if fragment.TypeCondition != objType {
		t.Error("Inline fragment was for wrong type")
		return
	}

	// make sure the selection set is what we expected
	assert.Equal(t, selection, fragment.SelectionSet)
}

func TestExecutorGetPointData(t *testing.T) {
	table := []struct {
		point string
		data  *extractorPointData
	}{
		{"foo:2", &extractorPointData{Field: "foo", Index: 2, ID: ""}},
		{"foo#3", &extractorPointData{Field: "foo", Index: -1, ID: "3"}},
		{"foo:2#3", &extractorPointData{Field: "foo", Index: 2, ID: "3"}},
	}

	for _, row := range table {
		pointData, err := executorGetPointData(row.point)
		if err != nil {
			t.Error(err.Error())
			return
		}

		assert.Equal(t, row.data, pointData)
	}
}

func TestFindInsertionPoint_stitchIntoObject(t *testing.T) {
	// we want the list of insertion points that point to
	planInsertionPoint := []string{"users", "photoGallery", "author", "firstName"}

	// pretend we are in the middle of stitching a larger object
	startingPoint := [][]string{{"users:0"}}

	// there are 6 total insertion points in this example
	finalInsertionPoint := [][]string{
		// photo 0 is liked by 2 users whose firstName we have to resolve
		{"users:0", "photoGallery:0", "author#1", "firstName"},
		// photo 1 is liked by 3 users whose firstName we have to resolve
		{"users:0", "photoGallery:1", "author#2", "firstName"},
		// photo 2 is liked by 1 user whose firstName we have to resolve
		{"users:0", "photoGallery:2", "author#3", "firstName"},
	}

	// the selection we're going to make
	stepSelectionSet := ast.SelectionSet{
		&ast.Field{
			Name: "photoGallery",
			Definition: &ast.FieldDefinition{
				Type: ast.ListType(ast.NamedType("Photo", &ast.Position{}), &ast.Position{}),
			},
			SelectionSet: ast.SelectionSet{
				&ast.Field{
					Name: "author",
					Definition: &ast.FieldDefinition{
						Type: ast.NamedType("User", &ast.Position{}),
					},
					SelectionSet: ast.SelectionSet{
						&ast.Field{
							Name: "totalLikes",
							Definition: &ast.FieldDefinition{
								Type: ast.NamedType("Int", &ast.Position{}),
							},
						},
						&ast.Field{
							Name: "id",
							Definition: &ast.FieldDefinition{
								Type: ast.NamedType("ID", &ast.Position{}),
							},
						},
					},
				},
			},
		},
	}

	// the result of the step
	result := map[string]interface{}{
		"photoGallery": []interface{}{
			map[string]interface{}{
				"author": map[string]interface{}{
					"id": "1",
				},
			},
			map[string]interface{}{
				"author": map[string]interface{}{
					"id": "2",
				},
			},
			map[string]interface{}{
				"author": map[string]interface{}{
					"id": "3",
				},
			},
		},
	}

	generatedPoint, err := findInsertionPoints(planInsertionPoint, stepSelectionSet, result, startingPoint, false)
	if err != nil {
		t.Error(t, err)
		return
	}

	assert.Equal(t, finalInsertionPoint, generatedPoint)

}

func TestFindInsertionPoint_handlesNullObjects(t *testing.T) {
	t.Skip("Not yet implemented")
}
