package gateway

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/alecaivazis/graphql-gateway/graphql"
	"github.com/vektah/gqlparser/ast"
)

// Executor is responsible for executing a query plan against the remote
// schemas and returning the result
type Executor interface {
	Execute(plan *QueryPlan, variables map[string]interface{}) (map[string]interface{}, error)
}

// ParallelExecutor executes the given query plan by starting at the root of the plan and
// walking down the path stitching the results together
type ParallelExecutor struct{}

type queryExecutionResult struct {
	InsertionPoint []string
	Result         map[string]interface{}
	StripNode      bool
}

// Execute returns the result of the query plan
func (executor *ParallelExecutor) Execute(plan *QueryPlan, variables map[string]interface{}) (map[string]interface{}, error) {
	// a place to store the result
	result := map[string]interface{}{}

	// a channel to receive query results
	resultCh := make(chan queryExecutionResult, 10)
	// defer close(resultCh)

	// a wait group so we know when we're done with all of the steps
	stepWg := &sync.WaitGroup{}

	// and a channel for errors
	errCh := make(chan error, 10)
	// defer close(errCh)

	// a channel to close the goroutine
	closeCh := make(chan bool)
	// defer close(closeCh)

	// if there are no steps after the root step, there is a problem
	if len(plan.RootStep.Then) == 0 {
		return nil, errors.New("was given empty plan")
	}

	// the root step could have multiple steps that have to happen
	for _, step := range plan.RootStep.Then {
		stepWg.Add(1)
		go executeStep(plan, step, []string{}, variables, resultCh, errCh, stepWg)
	}

	// start a goroutine to add results to the list
	go func() {
	ConsumptionLoop:
		for {
			select {
			// we have a new result
			case payload := <-resultCh:
				log.Debug("Inserting result into ", payload.InsertionPoint)
				log.Debug("Result: ", payload.Result)
				// we have to grab the value in the result and write it to the appropriate spot in the
				// acumulator.

				// the path that we want out of the result
				path := []string{}
				// if we want to strip node from the response
				if payload.StripNode {
					path = append(path, "node")
					path = append(path, payload.InsertionPoint[max(len(payload.InsertionPoint)-1, 0):][0])
				}

				// get the result from the response that we have to stitch there
				queryResult, err := executorExtractValue(payload.Result, path)
				if err != nil {
					errCh <- err
					continue ConsumptionLoop
				}
				log.Debug("raw value: ", queryResult)

				log.Debug("Inserting result into ", payload.InsertionPoint)
				log.Debug("Result: ", queryResult)
				// copy the result into the accumulator
				err = executorInsertObject(result, payload.InsertionPoint, queryResult)
				if err != nil {
					errCh <- err
					continue ConsumptionLoop
				}

				log.Debug("Done")
				// one of the queries is done
				stepWg.Done()

			// we're done
			case <-closeCh:
				return
			}
		}
	}()

	// there are 2 possible options:
	// - either the wait group finishes
	// - we get a messsage over the error chan

	// in order to wait for either, let's spawn a go routine
	// that waits until all of the steps are built and notifies us when its done
	doneCh := make(chan bool)
	// defer close(doneCh)

	go func() {
		// when the wait group is finished
		stepWg.Wait()
		// push a value over the channel
		doneCh <- true
	}()

	// wait for either the error channel or done channel
	select {
	// there was an error
	case err := <-errCh:
		log.Warn(fmt.Sprintf("Ran into execution error: %s", err.Error()))
		closeCh <- true
		// bubble the error up
		return nil, err
	// we are done
	case <-doneCh:
		closeCh <- true
		// we're done here
		return result, nil
	}
}

// TODO: ugh... so... many... variables...
func executeStep(
	plan *QueryPlan,
	step *QueryPlanStep,
	insertionPoint []string,
	queryVariables map[string]interface{},
	resultCh chan queryExecutionResult,
	errCh chan error,
	stepWg *sync.WaitGroup,
) {
	log.Debug("")
	log.Debug("Executing step to be inserted in ", step.ParentType, " ", insertionPoint)

	log.Debug(step.SelectionSet)

	// if this step has a selection, the resulting steps will end up being inserted at a particular object
	// we need to make sure that we ask for the id of the object
	if len(step.SelectionSet) > 0 {
		// each selection set that is the parent of another query must ask for the id
		for _, nextStep := range step.Then {
			// the next query will go
			path := nextStep.InsertionPoint[:max(len(nextStep.InsertionPoint)-1, 0)]
			log.Debug("Step has children. Need to add ids ", path, nextStep.InsertionPoint)

			// the selection set we need to add `id` to
			target := step.SelectionSet
			var targetField *ast.Field

			for _, point := range path {
				// look for the selection with that name
				for _, selection := range selectedFields(target) {
					// if we still have to walk down the selection but we found the right branch
					if selection.Name == point {
						target = selection.SelectionSet
						targetField = selection
						break
					}
				}
			}

			// if we couldn't find the target
			if target == nil {
				errCh <- fmt.Errorf("Could not find field to add id to. insertion point: %v", path)
				return
			}

			// if the target does not currently ask for id we need to add it
			addID := true
			for _, selection := range selectedFields(target) {
				if selection.Name == "id" {
					addID = false
					break
				}
			}

			// add the ID to the selection set if necessary
			if addID {
				target = append(target, &ast.Field{
					Name: "id",
				})
			}

			// make sure the selection set contains the id
			targetField.SelectionSet = target
		}
	}

	// log the query
	log.QueryPlanStep(step)

	// the id of the object we are query is defined by the last step in the realized insertion point
	id := ""
	if len(insertionPoint) > 1 {
		head := insertionPoint[max(len(insertionPoint)-2, 0)]

		// get the data of the point
		pointData, err := executorGetPointData(head)
		if err != nil {
			errCh <- err
			return
		}

		// reassign the id
		id = pointData.ID
	}
	// the list of variables and their definitions that pertain to this query
	variableDefs := ast.VariableDefinitionList{}
	variables := map[string]interface{}{}

	// we need to grab the variable definitions and values for each variable in the step
	for variable := range step.Variables {
		fmt.Println("looking for ", variable)
		// add the definition
		variableDefs = append(variableDefs, plan.Variables.ForName(variable))
		// and the value if it exists
		if value, ok := queryVariables[variable]; ok {
			fmt.Println("found value")
			variables[variable] = value
		}
		fmt.Println(variableDefs)
	}

	// generate the query that we have to send for this step
	query := executorBuildQuery(step.ParentType, id, variableDefs, step.SelectionSet)
	queryStr, err := graphql.PrintQuery(query)
	if err != nil {
		errCh <- err
		return
	}
	log.Debug("Sending network query: ", queryStr)
	// if there is no queryer
	if step.Queryer == nil {
		errCh <- errors.New("could not find queryer for step")
		return
	}
	// execute the query
	queryResult := map[string]interface{}{}
	err = step.Queryer.Query(&graphql.QueryInput{
		Query:         queryStr,
		QueryDocument: query,
		Variables:     variables,
	}, &queryResult)
	if err != nil {
		errCh <- err
		return
	}

	// NOTE: this insertion point could point to a list of values. If it did, we have to have
	//       passed it to the this invocation of this function. It is safe to trust this
	//       InsertionPoint as the right place to insert this result.

	// this is the only place we know for sure if we have to strip the node
	stripNode := step.ParentType != "Query"

	// if there are next steps
	if len(step.Then) > 0 {
		log.Debug("Kicking off child queries")
		// we need to find the ids of the objects we are inserting into and then kick of the worker with the right
		// insertion point. For lists, insertion points look like: ["user", "friends:0", "catPhotos:0", "owner"]
		for _, dependent := range step.Then {
			// the insertion point for this step needs to go one behind so we can build up a list if the root is one
			clip := max(len(insertionPoint)-1, 0)

			// log.Debug("Looking for insertion points for ", dependent.InsertionPoint, "\n\n")
			insertPoints, err := executorFindInsertionPoints(dependent.InsertionPoint, step.SelectionSet, queryResult, [][]string{insertionPoint[:clip]}, stripNode)
			if err != nil {
				errCh <- err
				return
			}

			// this dependent needs to fire for every object that the insertion point references
			for _, insertionPoint := range insertPoints {
				log.Info("Spawn ", insertionPoint)
				stepWg.Add(1)
				go executeStep(plan, dependent, insertionPoint, queryVariables, resultCh, errCh, stepWg)
			}
		}
	}

	log.Debug("Pushing Result ", insertionPoint)
	// send the result to be stitched in with our accumulator
	resultCh <- queryExecutionResult{
		InsertionPoint: insertionPoint,
		Result:         queryResult,
		StripNode:      stripNode,
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// executorFindInsertionPoints returns the list of insertion points where this step should be executed.
func executorFindInsertionPoints(targetPoints []string, selectionSet ast.SelectionSet, result map[string]interface{}, startingPoints [][]string, stripNode bool) ([][]string, error) {

	// log.Debug("Looking for insertion points. target: ", targetPoints)
	oldBranch := startingPoints
	for _, branch := range oldBranch {
		if len(branch) > 1 {
			branch = branch[:max(len(branch)-1, 1)]
		}
	}

	// track the root of the selection set while  we walk
	selectionSetRoot := selectionSet

	// a place to refer to parts of the results
	resultChunk := result

	// the index to start at
	startingIndex := 0
	if len(oldBranch) > 0 {
		startingIndex = len(oldBranch[0])
	}

	log.Debug("result ", resultChunk)

	// if our starting point is []string{"users:0", "photoGallery"} then we know everything up until photoGallery
	// is along the path of the steps insertion point
	for pointI := startingIndex; pointI < len(targetPoints); pointI++ {
		// the point in the steps insertion path that we want to add
		point := targetPoints[pointI]

		// log.Debug("Looking for ", point)

		// if we are at the last field, just add it
		if pointI == len(targetPoints)-1 {
			for i, points := range oldBranch {
				oldBranch[i] = append(points, point)
			}
		} else {
			// track wether we found a selection
			foundSelection := false

			// there should be a field in the root selection set that has the target point
			for _, selection := range selectedFields(selectionSetRoot) {
				// if the selection has the right name we need to add it to the list
				if selection.Alias == point || selection.Name == point {
					// log.Debug("Found Selection for: ", point)
					// log.Debug("Strip node: ", stripNode)
					// log.Debug("Result Chunk: ", resultChunk)
					// make sure we are looking at the top of the selection set next time
					selectionSetRoot = selection.SelectionSet

					var value = resultChunk
					// if we are supposed to strip the top level node
					if stripNode {
						// grab the value of the top level node
						nodeValue, ok := value["node"]
						if !ok {
							return nil, errors.New("Could not find top level node value")
						}

						// make sure the node value is an object
						objValue, ok := nodeValue.(map[string]interface{})
						if !ok {
							return nil, errors.New("node value was not an object")
						}

						value = objValue
					}
					// the bit of result chunk with the appropriate key should be a list
					rootValue, ok := value[point]
					if !ok {
						return nil, errors.New("Root value of result chunk could not be found")
					}

					// get the type of the object in question
					selectionType := selection.Definition.Type
					// if the type is a list
					if selectionType.Elem != nil {
						log.Debug("Selection is a list")
						// make sure the root value is a list
						rootList, ok := rootValue.([]interface{})
						if !ok {
							return nil, fmt.Errorf("Root value of result chunk was not a list: %v", rootValue)
						}

						// build up a new list of insertion points
						newInsertionPoints := [][]string{}

						// each value in this list contributes an insertion point
						for entryI, iEntry := range rootList {
							resultEntry, ok := iEntry.(map[string]interface{})
							if !ok {
								return nil, errors.New("entry in result wasn't a map")
							}
							// the point we are going to add to the list
							entryPoint := fmt.Sprintf("%s:%v", selection.Name, entryI)
							// log.Debug("Adding ", entryPoint, " to list")

							newBranchSet := make([][]string, len(oldBranch))
							copy(newBranchSet, oldBranch)

							// if we are adding to an existing branch
							if len(newBranchSet) > 0 {
								// add the path to the end of this for the entry we just added
								for i, newBranch := range newBranchSet {
									// if we are looking at the second to last thing in the insertion list
									if pointI == len(targetPoints)-2 {
										// look for an id
										id, ok := resultEntry["id"]
										if !ok {
											return nil, errors.New("Could not find the id for elements in target list")
										}

										// add the id to the entry so that the executor can use it to form its query
										entryPoint = fmt.Sprintf("%s#%v", entryPoint, id)

									}

									// add the point for this entry in the list
									newBranchSet[i] = append(newBranch, entryPoint)
								}
							} else {
								newBranchSet = append(newBranchSet, []string{entryPoint})
							}

							// compute the insertion points for that entry
							entryInsertionPoints, err := executorFindInsertionPoints(targetPoints, selectionSetRoot, resultEntry, newBranchSet, false)
							if err != nil {
								return nil, err
							}

							for _, point := range entryInsertionPoints {
								// add the list of insertion points to the acumulator
								newInsertionPoints = append(newInsertionPoints, point)
							}
						}

						// return the flat list of insertion points created by our children
						return newInsertionPoints, nil
					}

					// we are encountering something that isn't a list so it must be an object or a scalar
					// regardless, we just need to add the point to the end of each list
					for i, points := range oldBranch {
						oldBranch[i] = append(points, point)
					}

					if pointI == len(targetPoints)-2 {
						// the root value could be a list in which case the id is the id of the corresponding entry
						// or the root value could be an object in which case the id is the id of the root value

						// if the root value is a list
						if rootList, ok := rootValue.([]interface{}); ok {
							for i := range oldBranch {
								entry, ok := rootList[i].(map[string]interface{})
								if !ok {
									return nil, errors.New("Item in root list isn't a map")
								}

								// look up the id of the object
								id, ok := entry["id"]
								if !ok {
									return nil, errors.New("Could not find the id for the object")
								}

								// log.Debug("Adding id to ", oldBranch[i][pointI])

								oldBranch[i][pointI] = fmt.Sprintf("%s:%v#%v", oldBranch[i][pointI], i, id)

							}
						} else {
							rootObj, ok := rootValue.(map[string]interface{})
							if !ok {
								return nil, fmt.Errorf("Root value of result chunk was not an object. Point: %v Value: %v", point, rootValue)
							}

							for i := range oldBranch {
								// look up the id of the object
								id := rootObj["id"]
								if !ok {
									return nil, errors.New("Could not find the id for the object")
								}

								oldBranch[i][pointI] = fmt.Sprintf("%s#%v", oldBranch[i][pointI], id)
							}
						}
					}

					// we're done looking through the selection set
					foundSelection = true
					break
				}

			}

			if !foundSelection {
				return nil, fmt.Errorf("Could not find selection for %v", point)
			}
		}
	}

	// return the aggregation
	return oldBranch, nil
}

func executorExtractValue(source map[string]interface{}, path []string) (interface{}, error) {
	// a pointer to the objects we are modifying
	var recent interface{} = source
	log.Debug("Pulling ", path, " from ", source)

	for i, point := range path[:len(path)] {
		// if the point designates an element in the list
		if strings.Contains(point, ":") {
			pointData, err := executorGetPointData(point)
			if err != nil {
				return nil, err
			}

			recentObj, ok := recent.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("List was not a child of an object. %v", pointData)
			}

			// if the field does not exist
			if _, ok := recentObj[pointData.Field]; !ok {
				recentObj[pointData.Field] = []interface{}{}
			}

			// it should be a list
			field := recentObj[pointData.Field]

			targetList, ok := field.([]interface{})
			if !ok {
				return nil, fmt.Errorf("did not encounter a list when expected. Point: %v. Field: %v. Result %v", point, pointData.Field, field)
			}

			// if the field exists but does not have enough spots
			if len(targetList) <= pointData.Index {
				for i := len(targetList) - 1; i < pointData.Index; i++ {
					targetList = append(targetList, map[string]interface{}{})
				}

				// update the list with what we just made
				recentObj[pointData.Field] = targetList
			}

			// focus on the right element
			recent = targetList[pointData.Index]
		} else {
			// it's possible that there's an id
			pointData, err := executorGetPointData(point)
			if err != nil {
				return nil, err
			}

			pointField := pointData.Field

			recentObj, ok := recent.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("thisone, Target was not an object. %v, %v", pointData, recent)
			}

			// we are add an object value
			targetObject := recentObj[pointField]

			if i != len(path)-1 && targetObject == nil {
				recentObj[pointField] = map[string]interface{}{}
			}
			// if we haven't created an object there with that field
			if targetObject == nil {
				recentObj[pointField] = map[string]interface{}{}
			}

			// look there next
			recent = recentObj[pointField]
		}
	}

	return recent, nil
}

func executorInsertObject(target map[string]interface{}, path []string, value interface{}) error {
	if len(path) > 0 {
		head := path[len(path)-1]
		// the path to the key we want to set
		tail := path[:max(len(path)-1, 0)]

		// a pointer to the objects we are modifying
		obj, err := executorExtractValue(target, tail)
		if err != nil {
			return err
		}

		valueObj, ok := obj.(map[string]interface{})
		if !ok {
			return errors.New("something went wrong")
		}

		// if the head points to a list
		if strings.Contains(head, ":") {
			// {head} is a key for a list, and value needs to go in the right place
			pointData, err := executorGetPointData(head)
			if err != nil {
				return err
			}

			// if there is no list at that location
			if _, ok := valueObj[pointData.Field]; !ok {
				valueObj[pointData.Field] = []interface{}{}
			}

			// if its not a list
			valueList, ok := valueObj[pointData.Field].([]interface{})
			if !ok {
				return errors.New("Found a non-list at the insertion point")
			}

			if len(valueList) <= pointData.Index {
				// make sure that the list has enough entries
				for i := max(len(valueList)-1, 0); i <= pointData.Index; i++ {
					valueList = append(valueList, map[string]interface{}{})
				}
			}

			field := valueList[pointData.Index]
			mapField, ok := field.(map[string]interface{})

			objValue, ok := value.(map[string]interface{})
			if ok {
				// make sure we update the object at that location with each value
				for k, v := range objValue {
					mapField[k] = v
				}
			}

			// re-assign the list back to the object
			valueObj[pointData.Field] = valueList

		} else {
			// we are just assigning a value
			valueObj[head] = value
		}
	} else {
		valueObj, ok := value.(map[string]interface{})
		if !ok {
			return errors.New("something went wrong")
		}

		for key, value := range valueObj {
			target[key] = value
		}
	}
	return nil
}

type extractorPointData struct {
	Field string
	Index int
	ID    string
}

func executorGetPointData(point string) (*extractorPointData, error) {
	field := point
	index := -1
	id := ""

	// points come in the form <field>:<index>#<id> and each of index or id is optional
	if strings.Contains(point, "#") {
		idData := strings.Split(point, "#")
		if len(idData) == 2 {
			id = idData[1]
		}

		// use the index data without the id
		field = idData[0]
	}

	if strings.Contains(field, ":") {
		indexData := strings.Split(field, ":")
		indexValue, err := strconv.ParseInt(indexData[1], 0, 32)
		if err != nil {
			return nil, err
		}

		index = int(indexValue)
		field = indexData[0]
	}

	return &extractorPointData{
		Field: field,
		Index: index,
		ID:    id,
	}, nil
}

func executorBuildQuery(parentType string, parentID string, variables ast.VariableDefinitionList, selectionSet ast.SelectionSet) *ast.OperationDefinition {
	log.Debug("Querying ", parentType, " ", parentID)
	// build up an operation for the query
	operation := &ast.OperationDefinition{
		Operation:           ast.Query,
		VariableDefinitions: variables,
	}

	// if we are querying the top level Query all we need to do is add
	// the selection set at the root
	if parentType == "Query" {
		operation.SelectionSet = selectionSet
	} else {
		// if we are not querying the top level then we have to embed the selection set
		// under the node query with the right id as the argument

		// we want the operation to have the equivalent of
		// {
		//	 	node(id: parentID) {
		//	 		... on parentType {
		//	 			selection
		//	 		}
		//	 	}
		// }
		operation.SelectionSet = ast.SelectionSet{
			&ast.Field{
				Name: "node",
				Arguments: ast.ArgumentList{
					&ast.Argument{
						Name: "id",
						Value: &ast.Value{
							Kind: ast.StringValue,
							Raw:  parentID,
						},
					},
				},
				SelectionSet: ast.SelectionSet{
					&ast.InlineFragment{
						TypeCondition: parentType,
						SelectionSet:  selectionSet,
					},
				},
			},
		}
	}
	log.Debug("Build Query")

	// add the operation to a QueryDocument
	return operation
}
