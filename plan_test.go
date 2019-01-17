package gateway

import (
	"fmt"
	"testing"

	"github.com/alecaivazis/graphql-gateway/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/ast"
)

func TestPlanQuery_singleRootField(t *testing.T) {
	// the location for the schema
	location := "url1"

	// the location map for fields for this query
	locations := FieldURLMap{}
	locations.RegisterURL("Query", "foo", location)

	schema, _ := graphql.LoadSchema(`
		type Query {
			foo: Boolean
		}
	`)

	// compute the plan for a query that just hits one service
	plans, err := (&MinQueriesPlanner{}).Plan(`
		{
			foo
		}
	`, schema, locations)
	// if something went wrong planning the query
	if err != nil {
		// the test is over
		t.Errorf("encountered error when building schema: %s", err.Error())
		return
	}

	// the first selection is the only one we care about
	root := plans[0].RootStep.Then[0]
	// there should only be one selection
	if len(root.SelectionSet) != 1 {
		t.Error("encountered the wrong number of selections under root step")
		return
	}
	rootField := selectedFields(root.SelectionSet)[0]

	// make sure that the first step is pointed at the right place
	queryer := root.Queryer.(*graphql.NetworkQueryer)
	assert.Equal(t, location, queryer.URL)

	// we need to be asking for Query.foo
	assert.Equal(t, rootField.Name, "foo")

	// there should be anything selected underneath it
	assert.Len(t, rootField.SelectionSet, 0)
}

func TestPlanQuery_singleRootObject(t *testing.T) {
	// the location for the schema
	location := "url1"

	// the location map for fields for this query
	locations := FieldURLMap{}
	locations.RegisterURL("Query", "allUsers", location)
	locations.RegisterURL("User", "firstName", location)
	locations.RegisterURL("User", "friends", location)

	schema, _ := graphql.LoadSchema(`
		type User {
			firstName: String!
			friends: [User!]!
		}

		type Query {
			allUsers: [User!]!
		}
	`)

	// compute the plan for a query that just hits one service
	selections, err := (&MinQueriesPlanner{}).Plan(`
		{
			allUsers {
				firstName
				friends {
					firstName
					friends {
						firstName
					}
				}
			}
		}
	`, schema, locations)
	// if something went wrong planning the query
	if err != nil {
		// the test is over
		t.Errorf("encountered error when building schema: %s", err.Error())
		return
	}

	// the first selection is the only one we care about
	rootStep := selections[0].RootStep.Then[0]

	// there should only be one selection
	if len(rootStep.SelectionSet) != 1 {
		t.Error("encountered the wrong number of selections under root step")
		return
	}

	rootField := selectedFields(rootStep.SelectionSet)[0]

	// make sure that the first step is pointed at the right place
	queryer := rootStep.Queryer.(*graphql.NetworkQueryer)
	assert.Equal(t, location, queryer.URL)

	// we need to be asking for allUsers
	assert.Equal(t, rootField.Name, "allUsers")

	// grab the field from the top level selection
	field, ok := rootField.SelectionSet[0].(*ast.Field)
	if !ok {
		t.Error("Did not get a field out of the allUsers selection")
		return
	}
	// and from all users we need to ask for their firstName
	assert.Equal(t, "firstName", field.Name)
	assert.Equal(t, "String!", field.Definition.Type.Dump())

	// we also should have asked for the friends object
	friendsField, ok := rootField.SelectionSet[1].(*ast.Field)
	if !ok {
		t.Error("Did not get a friends field out of the allUsers selection")
	}
	// and from all users we need to ask for their firstName
	assert.Equal(t, "friends", friendsField.Name)
	// look at the selection we've made of friends
	firstNameField, ok := friendsField.SelectionSet[0].(*ast.Field)
	if !ok {
		t.Error("Did not get a field out of the allUsers selection")
	}
	assert.Equal(t, "firstName", firstNameField.Name)

	// there should be a second field for friends
	friendsInnerField, ok := friendsField.SelectionSet[1].(*ast.Field)
	if !ok {
		t.Error("Did not get an  inner friends out of the allUsers selection")
	}
	assert.Equal(t, "friends", friendsInnerField.Name)

	// and a field below it for their firstName
	firstNameInnerField, ok := friendsInnerField.SelectionSet[0].(*ast.Field)
	if !ok {
		t.Error("Did not get an  inner firstName out of the allUsers selection")
	}
	assert.Equal(t, "firstName", firstNameInnerField.Name)

}

func TestPlanQuery_subGraphs(t *testing.T) {
	schema, _ := graphql.LoadSchema(`
		type User {
			firstName: String!
			catPhotos: [CatPhoto!]!
		}

		type CatPhoto {
			URL: String!
			owner: User!
		}

		type Query {
			allUsers: [User!]!
		}
	`)

	// the location of the user service
	userLocation := "user-location"
	// the location of the cat service
	catLocation := "cat-location"

	// the location map for fields for this query
	locations := FieldURLMap{}
	locations.RegisterURL("Query", "allUsers", userLocation)
	locations.RegisterURL("User", "firstName", userLocation)
	locations.RegisterURL("User", "catPhotos", catLocation)
	locations.RegisterURL("CatPhoto", "URL", catLocation)
	locations.RegisterURL("CatPhoto", "owner", userLocation)

	plans, err := (&MinQueriesPlanner{}).Plan(`
		{
			allUsers {
				firstName
				catPhotos {
					URL
					owner {
						firstName
					}
				}
			}
		}
	`, schema, locations)
	// if something went wrong planning the query
	if err != nil {
		// the test is over
		t.Errorf("encountered error when building schema: %s", err.Error())
		return
	}

	// there are 3 steps of a single plan that we care about
	// the first step is grabbing allUsers and their firstName from the user service
	// the second step is grabbing User catPhotos from the cat service
	// the third step is grabb CatPhoto.owner.firstName from the user service from the user service

	// the first step should have all users
	firstStep := plans[0].RootStep.Then[0]
	// make sure we are grabbing values off of Query since its the root
	assert.Equal(t, "Query", firstStep.ParentType)

	// make sure there's a selection set
	if len(firstStep.SelectionSet) != 1 {
		t.Error("first step did not have a selection set")
		return
	}
	firstField := selectedFields(firstStep.SelectionSet)[0]
	// it is resolved against the user service
	queryer := firstStep.Queryer.(*graphql.NetworkQueryer)
	assert.Equal(t, userLocation, queryer.URL)

	// make sure it is for allUsers
	assert.Equal(t, "allUsers", firstField.Name)

	// all users should have only one selected value since `catPhotos` is from another service
	if len(firstField.SelectionSet) > 1 {
		for _, selection := range selectedFields(firstField.SelectionSet) {
			fmt.Println(selection.Name)
		}
		t.Error("Encountered too many fields on allUsers selection set")
		return
	}

	// grab the field from the top level selection
	field, ok := firstField.SelectionSet[0].(*ast.Field)
	if !ok {
		t.Error("Did not get a field out of the allUsers selection")
		return
	}
	// and from all users we need to ask for their firstName
	assert.Equal(t, "firstName", field.Name)
	assert.Equal(t, "String!", field.Definition.Type.Dump())

	// the second step should ask for the cat photo fields
	if len(firstStep.Then) != 1 {
		t.Errorf("Encountered the wrong number of steps after the first one %v", len(firstStep.Then))
		return
	}
	secondStep := firstStep.Then[0]
	// make sure we will insert the step in the right place
	assert.Equal(t, []string{"allUsers"}, secondStep.InsertionPoint)

	// make sure we are grabbing values off of User since we asked for User.catPhotos
	assert.Equal(t, "User", secondStep.ParentType)
	// we should be going to the catePhoto servie
	queryer = secondStep.Queryer.(*graphql.NetworkQueryer)
	assert.Equal(t, catLocation, queryer.URL)
	// we should only want one field selected
	if len(secondStep.SelectionSet) != 1 {
		t.Errorf("Did not have the right number of subfields of User.catPhotos: %v", len(secondStep.SelectionSet))
		return
	}

	// make sure we selected the catPhotos field
	selectedSecondField := selectedFields(secondStep.SelectionSet)[0]
	assert.Equal(t, "catPhotos", selectedSecondField.Name)

	// we should have also asked for one field underneath
	secondSubSelection := selectedFields(selectedSecondField.SelectionSet)
	if len(secondSubSelection) != 1 {
		t.Error("Encountered the incorrect number of fields selected under User.catPhotos")
	}
	secondSubSelectionField := secondSubSelection[0]
	assert.Equal(t, "URL", secondSubSelectionField.Name)

	// the third step should ask for the User.firstName
	if len(secondStep.Then) != 1 {
		t.Errorf("Encountered the wrong number of steps after the second one %v", len(secondStep.Then))
		return
	}
	thirdStep := secondStep.Then[0]
	// make sure we will insert the step in the right place
	assert.Equal(t, []string{"allUsers", "catPhotos"}, thirdStep.InsertionPoint)

	// make sure we are grabbing values off of User since we asked for User.catPhotos
	assert.Equal(t, "CatPhoto", thirdStep.ParentType)
	// we should be going to the catePhoto service
	queryer = thirdStep.Queryer.(*graphql.NetworkQueryer)
	assert.Equal(t, userLocation, queryer.URL)
	// make sure we will insert the step in the right place
	assert.Equal(t, []string{"allUsers", "catPhotos"}, thirdStep.InsertionPoint)

	// we should only want one field selected
	if len(thirdStep.SelectionSet) != 1 {
		t.Errorf("Did not have the right number of subfields of User.catPhotos: %v", len(thirdStep.SelectionSet))
		return
	}

	// make sure we selected the catPhotos field
	selectedThirdField := selectedFields(thirdStep.SelectionSet)[0]
	assert.Equal(t, "owner", selectedThirdField.Name)

	// we should have also asked for one field underneath
	thirdSubSelection := selectedFields(selectedThirdField.SelectionSet)
	if len(thirdSubSelection) != 1 {
		t.Error("Encountered the incorrect number of fields selected under User.catPhotos")
	}
	thirdSubSelectionField := thirdSubSelection[0]
	assert.Equal(t, "firstName", thirdSubSelectionField.Name)
}

func TestPlanQuery_preferParentLocation(t *testing.T) {

	schema, _ := graphql.LoadSchema(`
		type User {
			id: ID!
		}

		type Query {
			allUsers: [User!]!
		}
	`)

	// the location of the user service
	userLocation := "user-location"
	// the location of the cat service
	catLocation := "cat-location"

	// the location map for fields for this query
	locations := FieldURLMap{}
	locations.RegisterURL("Query", "allUsers", userLocation)
	// add the
	locations.RegisterURL("User", "id", catLocation)
	locations.RegisterURL("User", "id", userLocation)

	plans, err := (&MinQueriesPlanner{}).Plan(`
		{
			allUsers {
				id
			}
		}
	`, schema, locations)
	// if something went wrong planning the query
	if err != nil {
		// the test is over
		t.Errorf("encountered error when building schema: %s", err.Error())
		return
	}

	// there should only be 1 step to this query

	// the first step should have all users
	firstStep := plans[0].RootStep.Then[0]
	// make sure we are grabbing values off of Query since its the root
	assert.Equal(t, "Query", firstStep.ParentType)

	// make sure there's a selection set
	if len(firstStep.Then) != 0 {
		t.Errorf("There shouldn't be any dependent step on this one. Found %v.", len(firstStep.Then))
		return
	}
}

func TestPlanQuery_groupSiblings(t *testing.T) {
	schema, _ := graphql.LoadSchema(`
		type User {
			favoriteCatSpecies: String!
			catPhotos: [CatPhoto!]!
		}

		type CatPhoto {
			URL: String!
		}

		type Query {
			allUsers: [User!]!
		}
	`)

	// the location of the user service
	userLocation := "user-location"
	// the location of the cat service
	catLocation := "cat-location"

	// the location map for fields for this query
	locations := FieldURLMap{}
	locations.RegisterURL("Query", "allUsers", userLocation)
	locations.RegisterURL("User", "favoriteCatSpecies", catLocation)
	locations.RegisterURL("User", "catPhotos", catLocation)
	locations.RegisterURL("CatPhoto", "URL", catLocation)

	plans, err := (&MinQueriesPlanner{}).Plan(`
		{
			allUsers {
				favoriteCatSpecies
				catPhotos {
					URL
				}
			}
		}
	`, schema, locations)
	// if something went wrong planning the query
	if err != nil {
		// the test is over
		t.Errorf("encountered error when building schema: %s", err.Error())
		return
	}

	// there should be 2 steps to this plan.
	// the first queries Query.allUsers
	// the second queries User.favoriteCatSpecies and User.catPhotos

	// the first step should have all users
	firstStep := plans[0].RootStep.Then[0]
	// make sure we are grabbing values off of Query since its the root
	assert.Equal(t, "Query", firstStep.ParentType)

	// make sure there's a selection set
	if len(firstStep.Then) != 1 {
		t.Errorf("Encountered incorrect number of dependent steps on root. Expected 1 found %v", len(firstStep.Then))
		return
	}
}

func TestPlanQuery_stepVariables(t *testing.T) {
	// the query to test
	// query($id: ID!, $category: String!) {
	// 		user(id: $id) {
	// 			favoriteCatPhoto(category: $category) {
	// 				URL
	// 			}
	// 		}
	// }
	//
	// it should result in one query that depends on $id and the second one
	// which requires $category and $id

	// the location map for fields for this query
	locations := FieldURLMap{}
	locations.RegisterURL("Query", "user", "url1")
	locations.RegisterURL("User", "favoriteCatPhoto", "url2")
	locations.RegisterURL("CatPhoto", "URL", "url2")

	schema, _ := graphql.LoadSchema(`
		type User {
			favoriteCatPhoto(category: String!, owner: ID!): CatPhoto!
		}

		type CatPhoto {
			URL: String!
		}

		type Query {
			user(id: ID!): User
		}
	`)

	// compute the plan for a query that just hits one service
	plans, err := (&MinQueriesPlanner{}).Plan(`
		query($id: ID!, $category: String!) {
			user(id: $id) {
				favoriteCatPhoto(category: $category, owner:$id) {
					URL
				}
			}
		}
	`, schema, locations)
	// if something went wrong planning the query
	if err != nil {
		// the test is over
		t.Errorf("encountered error when building schema: %s", err.Error())
		return
	}

	// there is only one step
	firstStep := plans[0].RootStep.Then[0]
	// make sure it has the right variable dependencies
	assert.Equal(t, Set{"id": true}, firstStep.Variables)

	// there is a step after
	nextStep := firstStep.Then[0]
	// make sure it has the right variable dependencies
	assert.Equal(t, Set{"category": true, "id": true}, nextStep.Variables)

	// we need to have a query with id and category since id is passed to node
	if len(nextStep.QueryDocument.VariableDefinitions) != 2 {
		t.Errorf("Did not find the right number of variable definitions in the next step. Expected 2 found %v", len(nextStep.QueryDocument.VariableDefinitions))
		return
	}

	for _, definition := range nextStep.QueryDocument.VariableDefinitions {
		if definition.Variable != "id" && definition.Variable != "category" {
			t.Errorf("Encountered a variable with an unknown name: %v", definition.Variable)
			return
		}
	}
}

func TestExtractVariables(t *testing.T) {
	table := []struct {
		Name      string
		Arguments ast.ArgumentList
		Variables []string
	}{
		//  user(id: $id, name:$name) should extract ["id", "name"]
		{
			Name:      "Top Level arguments",
			Variables: []string{"id", "name"},
			Arguments: ast.ArgumentList{
				&ast.Argument{
					Name: "id",
					Value: &ast.Value{
						Kind: ast.Variable,
						Raw:  "id",
					},
				},
				&ast.Argument{
					Name: "name",
					Value: &ast.Value{
						Kind: ast.Variable,
						Raw:  "name",
					},
				},
			},
		},
		//  catPhotos(categories: [$a, "foo", $b]) should extract ["a", "b"]
		{
			Name:      "List nested arguments",
			Variables: []string{"a", "b"},
			Arguments: ast.ArgumentList{
				&ast.Argument{
					Name: "category",
					Value: &ast.Value{
						Kind: ast.ListValue,
						Children: ast.ChildValueList{
							&ast.ChildValue{
								Value: &ast.Value{
									Kind: ast.Variable,
									Raw:  "a",
								},
							},
							&ast.ChildValue{
								Value: &ast.Value{
									Kind: ast.StringValue,
									Raw:  "foo",
								},
							},
							&ast.ChildValue{
								Value: &ast.Value{
									Kind: ast.Variable,
									Raw:  "b",
								},
							},
						},
					},
				},
			},
		},
		//  users(favoriteMovieFilter: {category: $targetCategory, rating: $targetRating}) should extract ["targetCategory", "targetRating"]
		{
			Name:      "Object nested arguments",
			Variables: []string{"targetCategory", "targetRating"},
			Arguments: ast.ArgumentList{
				&ast.Argument{
					Name: "favoriteMovieFilter",
					Value: &ast.Value{
						Kind: ast.ObjectValue,
						Children: ast.ChildValueList{
							&ast.ChildValue{
								Name: "category",
								Value: &ast.Value{
									Kind: ast.Variable,
									Raw:  "targetCategory",
								},
							},
							&ast.ChildValue{
								Name: "rating",
								Value: &ast.Value{
									Kind: ast.Variable,
									Raw:  "targetRating",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, row := range table {
		t.Run(row.Name, func(t *testing.T) {
			assert.Equal(t, row.Variables, plannerExtractVariables(row.Arguments))
		})
	}
}

func TestApplyFragments_mergesFragments(t *testing.T) {
	// a selection set representing
	// {
	//      birthday
	// 		... on User {
	// 			firstName
	//			lastName
	// 			friends {
	// 				firstName
	// 			}
	// 		}
	//      ...SecondFragment
	// 	}
	//
	// 	fragment SecondFragment on User {
	// 		lastName
	// 		friends {
	// 			lastName
	//			friends {
	//				lastName
	//			}
	// 		}
	// 	}
	//
	//
	// should be flattened into
	// {
	//		birthday
	// 		firstName
	// 		lastName
	// 		friends {
	// 			firstName
	// 			lastName
	//			friends {
	//				lastName
	//			}
	// 		}
	// }
	selectionSet := ast.SelectionSet{
		&ast.Field{
			Name:  "birthday",
			Alias: "birthday",
			Definition: &ast.FieldDefinition{
				Type: ast.NamedType("DateTime", &ast.Position{}),
			},
		},
		&ast.FragmentSpread{
			Name: "SecondFragment",
		},
		&ast.InlineFragment{
			TypeCondition: "User",
			SelectionSet: ast.SelectionSet{
				&ast.Field{
					Name:  "lastName",
					Alias: "lastName",
					Definition: &ast.FieldDefinition{
						Type: ast.NamedType("String", &ast.Position{}),
					},
				},
				&ast.Field{
					Name:  "firstName",
					Alias: "firstName",
					Definition: &ast.FieldDefinition{
						Type: ast.NamedType("String", &ast.Position{}),
					},
				},
				&ast.Field{
					Name:  "friends",
					Alias: "friends",
					Definition: &ast.FieldDefinition{
						Type: ast.ListType(ast.NamedType("User", &ast.Position{}), &ast.Position{}),
					},
					SelectionSet: ast.SelectionSet{
						&ast.Field{
							Name:  "firstName",
							Alias: "firstName",
							Definition: &ast.FieldDefinition{
								Type: ast.NamedType("String", &ast.Position{}),
							},
						},
					},
				},
			},
		},
	}

	fragmentDefinition := ast.FragmentDefinitionList{
		&ast.FragmentDefinition{
			Name: "SecondFragment",
			SelectionSet: ast.SelectionSet{
				&ast.Field{
					Name:  "lastName",
					Alias: "lastName",
					Definition: &ast.FieldDefinition{
						Type: ast.NamedType("String", &ast.Position{}),
					},
				},
				&ast.Field{
					Name:  "friends",
					Alias: "friends",
					Definition: &ast.FieldDefinition{
						Type: ast.ListType(ast.NamedType("User", &ast.Position{}), &ast.Position{}),
					},
					SelectionSet: ast.SelectionSet{
						&ast.Field{
							Name:  "lastName",
							Alias: "lastName",
							Definition: &ast.FieldDefinition{
								Type: ast.NamedType("String", &ast.Position{}),
							},
						},
						&ast.Field{
							Name:  "friends",
							Alias: "friends",
							Definition: &ast.FieldDefinition{
								Type: ast.ListType(ast.NamedType("User", &ast.Position{}), &ast.Position{}),
							},
							SelectionSet: ast.SelectionSet{
								&ast.Field{
									Name:  "lastName",
									Alias: "lastName",
									Definition: &ast.FieldDefinition{
										Type: ast.NamedType("String", &ast.Position{}),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// should be flattened into
	// {
	//		birthday
	// 		firstName
	// 		lastName
	// 		friends {
	// 			firstName
	// 			lastName
	//			friends {
	//				lastName
	//			}
	// 		}
	// }

	// flatten the selection
	finalSelection, err := plannerApplyFragments(selectionSet, fragmentDefinition)
	if err != nil {
		t.Error(err.Error())
		return
	}
	fields := selectedFields(finalSelection)

	// make sure there are 4 fields at the root of the selection
	if len(fields) != 4 {
		t.Errorf("Encountered the incorrect number of selections: %v", len(fields))
		return
	}

	// get the selection set for birthday
	var birthdaySelection *ast.Field
	var firstNameSelection *ast.Field
	var lastNameSelection *ast.Field
	var friendsSelection *ast.Field

	for _, selection := range fields {
		switch selection.Alias {
		case "birthday":
			birthdaySelection = selection
		case "firstName":
			firstNameSelection = selection
		case "lastName":
			lastNameSelection = selection
		case "friends":
			friendsSelection = selection
		}
	}

	// make sure we got each definition
	assert.NotNil(t, birthdaySelection)
	assert.NotNil(t, firstNameSelection)
	assert.NotNil(t, lastNameSelection)
	assert.NotNil(t, friendsSelection)

	// make sure there are 3 selections under friends (firstName, lastName, and friends)
	if len(friendsSelection.SelectionSet) != 3 {
		t.Errorf("Encountered the wrong number of selections under .friends: len = %v)", len(friendsSelection.SelectionSet))
		for _, selection := range friendsSelection.SelectionSet {
			field, _ := selection.(*collectedField)
			t.Errorf("    %s", field.Name)
		}
		return
	}
}

func TestplannerBuildQuery_query(t *testing.T) {
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

	variables := ast.VariableDefinitionList{
		{
			Variable: "Foo",
			Type:     ast.NamedType("String", &ast.Position{}),
		},
	}

	// the query we're building goes to the top level Query object
	operation := plannerBuildQuery("Query", variables, selection)
	if operation == nil {
		t.Error("Did not receive a query.")
		return
	}

	// it should be a query
	assert.Equal(t, ast.Query, operation.Operation)
	assert.Equal(t, variables, operation.VariableDefinitions)

	// the selection set should be the same as what we passed in
	assert.Equal(t, selection, operation.SelectionSet)
}

func TestplannerBuildQuery_node(t *testing.T) {
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
	operation := plannerBuildQuery(objType, ast.VariableDefinitionList{}, selection)
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
	if argument.Value.Raw != "id" {
		t.Error("Did not pass the right id value to the node field")
		return
	}
	if argument.Value.Kind != ast.Variable {
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

func TestApplyFragments_skipAndIncludeDirectives(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestApplyFragments_leavesUnionsAndInterfaces(t *testing.T) {
	t.Skip("Not yet implemented")
}

func TestPlanQuery_multipleRootFields(t *testing.T) {
	t.Skip("Not implemented")
}

func TestPlanQuery_mutationsInSeries(t *testing.T) {
	t.Skip("Not implemented")
}

func TestPlanQuery_siblingFields(t *testing.T) {
	t.Skip("Not implemented")
}

func TestPlanQuery_duplicateFieldsOnEither(t *testing.T) {
	// make sure that if I have the same field defined on both schemas we dont create extraneous calls
	t.Skip("Not implemented")
}

func TestPlanQuery_groupsConflictingFields(t *testing.T) {
	// if I can find a field in 4 different services, look for the one I"m already going to
	t.Skip("Not implemented")
}
