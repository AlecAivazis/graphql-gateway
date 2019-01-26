package gateway

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql/introspection"
	"github.com/mitchellh/mapstructure"
	"github.com/vektah/gqlparser/ast"

	"github.com/alecaivazis/graphql-gateway/graphql"
)

// internalSchema is a graphql schema that exists at the gateway level and is merged with the
// other schemas that the gateway wraps.
var internalSchema *graphql.RemoteSchema

// internalSchemaLocation is the location that functions should take to identify a remote schema
// that points to the gateway's internal schema.
const internalSchemaLocation = "🎉"

// SchemaQueryer is a queryer that knows how to resolve a query according to a particular schema
type SchemaQueryer struct {
	Schema *ast.Schema
}

// Query takes a query definition and writes the result to the receiver
func (q *SchemaQueryer) Query(ctx context.Context, input *graphql.QueryInput, receiver interface{}) error {
	// a place to store the result
	result := map[string]interface{}{}

	// wrap the schema in something capable of introspection
	introspectionSchema := introspection.WrapSchema(q.Schema)

	// for local stuff we don't care about fragment directives
	querySelection, err := graphql.ApplyFragments(input.QueryDocument.Operations[0].SelectionSet, input.QueryDocument.Fragments)
	if err != nil {
		return err
	}

	for _, field := range graphql.SelectedFields(querySelection) {
		if field.Name == "__schema" {
			result[field.Alias] = q.introspectSchema(introspectionSchema, field.SelectionSet)
		}
		if field.Name == "__type" {
			// there is a name argument to look up the type
			name := field.Arguments.ForName("name").Value.Raw

			// look for the type with the designated name
			var introspectedType *introspection.Type
			for _, schemaType := range introspectionSchema.Types() {
				if *schemaType.Name() == name {
					introspectedType = &schemaType
					break
				}
			}

			// if we couldn't find the type
			if introspectedType == nil {
				result[field.Alias] = nil
			} else {
				// we found the type so introspect it
				result[field.Alias] = q.introspectType(introspectedType, field.SelectionSet)
			}
		}
	}

	// assign the result under the data key to the receiver
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName: "json",
		Result:  receiver,
	})
	if err != nil {
		return err
	}

	err = decoder.Decode(result)
	if err != nil {
		return err
	}

	return nil
}

func (q *SchemaQueryer) introspectSchema(schema *introspection.Schema, selectionSet ast.SelectionSet) map[string]interface{} {
	// a place to store the result
	result := map[string]interface{}{}

	for _, field := range graphql.SelectedFields(selectionSet) {
		switch field.Alias {
		case "types":
			result[field.Alias] = q.introspectTypeSlice(schema.Types(), field.SelectionSet)
		case "queryType":
			result[field.Alias] = q.introspectType(schema.QueryType(), field.SelectionSet)
		case "mutationType":
			result[field.Alias] = q.introspectType(schema.MutationType(), field.SelectionSet)
		case "subscriptionType":
			result[field.Alias] = q.introspectType(schema.SubscriptionType(), field.SelectionSet)
		case "directives":
			result[field.Alias] = q.introspectDirectiveSlice(schema.Directives(), field.SelectionSet)
		}
	}

	return result
}

func (q *SchemaQueryer) introspectType(schemaType *introspection.Type, selectionSet ast.SelectionSet) map[string]interface{} {
	if schemaType == nil {
		return nil
	}

	// a place to store the result
	result := map[string]interface{}{}

	for _, field := range graphql.SelectedFields(selectionSet) {
		// the default behavior is to ignore deprecated fields
		includeDeprecated := false
		if passedValue := field.Arguments.ForName("includeDeprecated"); passedValue != nil && passedValue.Value.Raw == "true" {
			includeDeprecated = true
		}

		switch field.Name {
		case "kind":
			result[field.Alias] = schemaType.Kind()
		case "name":
			result[field.Alias] = schemaType.Name()
		case "description":
			result[field.Alias] = schemaType.Description()
		case "fields":
			result[field.Alias] = q.introspectFieldSlice(schemaType.Fields(includeDeprecated), field.SelectionSet)
		case "interfaces":
			result[field.Alias] = q.introspectTypeSlice(schemaType.Interfaces(), field.SelectionSet)
		case "possibleTypes":
			result[field.Alias] = q.introspectTypeSlice(schemaType.PossibleTypes(), field.SelectionSet)
		case "enumValues":
			result[field.Alias] = q.introspectEnumValueSlice(schemaType.EnumValues(includeDeprecated), field.SelectionSet)
		case "inputFields":
			result[field.Alias] = q.introspectInputValueSlice(schemaType.InputFields(), field.SelectionSet)
		case "ofType":
			result[field.Alias] = q.introspectType(schemaType.OfType(), field.SelectionSet)
		}
	}
	return result
}

func (q *SchemaQueryer) introspectField(fieldDef introspection.Field, selectionSet ast.SelectionSet) map[string]interface{} {
	// a place to store the result
	result := map[string]interface{}{}

	for _, field := range graphql.SelectedFields(selectionSet) {
		switch field.Name {
		case "name":
			result[field.Alias] = fieldDef.Name
		case "description":
			result[field.Alias] = fieldDef.Description
		case "args":
			result[field.Alias] = q.introspectInputValueSlice(fieldDef.Args, field.SelectionSet)
		case "type":
			result[field.Alias] = q.introspectType(fieldDef.Type, field.SelectionSet)
		case "isDeprecated":
			result[field.Alias] = fieldDef.IsDeprecated()
		case "deprecationReason":
			result[field.Alias] = fieldDef.DeprecationReason()
		}
	}
	return result
}

func (q *SchemaQueryer) introspectEnumValue(definition *introspection.EnumValue, selectionSet ast.SelectionSet) map[string]interface{} {
	// a place to store the result
	result := map[string]interface{}{}

	for _, field := range graphql.SelectedFields(selectionSet) {
		switch field.Name {
		case "name":
			result[field.Alias] = definition.Name
		case "description":
			result[field.Alias] = definition.Description
		case "isDeprecated":
			result[field.Alias] = definition.IsDeprecated()
		case "deprecationReason":
			result[field.Alias] = definition.DeprecationReason()
		}
	}

	return result
}

func (q *SchemaQueryer) introspectDirective(directive introspection.Directive, selectionSet ast.SelectionSet) map[string]interface{} {
	// a place to store the result
	result := map[string]interface{}{}

	for _, field := range graphql.SelectedFields(selectionSet) {
		switch field.Name {
		case "name":
			result[field.Alias] = directive.Name
		case "description":
			result[field.Alias] = directive.Description
		case "args":
			result[field.Alias] = q.introspectInputValueSlice(directive.Args, field.SelectionSet)
		case "locations":
			result[field.Alias] = directive.Locations
		}
	}
	return result
}

func (q *SchemaQueryer) introspectInputValue(iv *introspection.InputValue, selectionSet ast.SelectionSet) map[string]interface{} {
	// a place to store the result
	result := map[string]interface{}{}

	for _, field := range graphql.SelectedFields(selectionSet) {
		switch field.Name {
		case "name":
			result[field.Alias] = iv.Name
		case "description":
			result[field.Alias] = iv.Description
		case "type":
			result[field.Alias] = q.introspectType(iv.Type, field.SelectionSet)
		}
	}

	return result
}

func (q *SchemaQueryer) introspectInputValueSlice(values []introspection.InputValue, selectionSet ast.SelectionSet) []map[string]interface{} {
	result := []map[string]interface{}{}

	// each type in the schema
	for _, field := range values {
		result = append(result, q.introspectInputValue(&field, selectionSet))
	}

	return result
}

func (q *SchemaQueryer) introspectFieldSlice(fields []introspection.Field, selectionSet ast.SelectionSet) []map[string]interface{} {
	result := []map[string]interface{}{}

	// each type in the schema
	for _, field := range fields {
		result = append(result, q.introspectField(field, selectionSet))
	}

	return result
}

func (q *SchemaQueryer) introspectEnumValueSlice(values []introspection.EnumValue, selectionSet ast.SelectionSet) []map[string]interface{} {
	result := []map[string]interface{}{}

	// each type in the schema
	for _, enumValue := range values {
		result = append(result, q.introspectEnumValue(&enumValue, selectionSet))
	}

	return result
}

func (q *SchemaQueryer) introspectTypeSlice(types []introspection.Type, selectionSet ast.SelectionSet) []map[string]interface{} {
	result := []map[string]interface{}{}

	// each type in the schema
	for _, field := range types {
		result = append(result, q.introspectType(&field, selectionSet))
	}

	return result
}

func (q *SchemaQueryer) introspectDirectiveSlice(directives []introspection.Directive, selectionSet ast.SelectionSet) []map[string]interface{} {
	result := []map[string]interface{}{}

	// each type in the schema
	for _, directive := range directives {
		result = append(result, q.introspectDirective(directive, selectionSet))
	}

	return result
}

func init() {
	// load the internal
	schema, err := graphql.LoadSchema(`
		type Query {
			_apiVersion: String
		}
	`)
	if schema == nil {
		panic(fmt.Sprintf("Syntax error in schema string: %s", err.Error()))
	}

	internalSchema = &graphql.RemoteSchema{
		URL:    internalSchemaLocation,
		Schema: schema,
	}
}
