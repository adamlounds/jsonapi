package jsonapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrBadJSONAPIStructTag is returned when the Struct field's JSON API
	// annotation is invalid.
	ErrBadJSONAPIStructTag = errors.New("Bad jsonapi struct tag format")
	// ErrBadJSONAPIID is returned when the Struct JSON API annotated "id" field
	// was not a valid numeric type.
	ErrBadJSONAPIID = errors.New(
		"id should be either string, int(8,16,32,64) or uint(8,16,32,64)")
	// ErrExpectedSlice is returned when a variable or arugment was expected to
	// be a slice of *Structs; MarshalMany will return this error when its
	// interface{} argument is invalid.
	ErrExpectedSlice = errors.New("models should be a slice of struct pointers")
)

// MarshalOnePayload writes a jsonapi response with one, with related records
// sideloaded, into "included" array. This method encodes a response for a
// single record only. Hence, data will be a single record rather than an array
// of records.  If you want to serialize many records, see, MarshalManyPayload.
//
// See UnmarshalPayload for usage example.
//
// model interface{} should be a pointer to a struct.
func MarshalOnePayload(w io.Writer, model interface{}) error {
	payload, err := MarshalOne(model)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return err
	}

	return nil
}

// MarshalOnePayloadWithoutIncluded writes a jsonapi response with one object,
// without the related records sideloaded into "included" array. If you want to
// serialize the relations into the "included" array see MarshalOnePayload.
//
// model interface{} should be a pointer to a struct.
func MarshalOnePayloadWithoutIncluded(w io.Writer, model interface{}) error {
	included := make(map[string]*Node)

	rootNode, err := VisitModelNode(model, &included, true)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(w).Encode(&OnePayload{Data: rootNode}); err != nil {
		return err
	}

	return nil
}

// MarshalOne does the same as MarshalOnePayload except it just returns the
// payload and doesn't write out results. Useful is you use your JSON rendering
// library.
func MarshalOne(model interface{}) (*OnePayload, error) {
	included := make(map[string]*Node)

	rootNode, err := VisitModelNode(model, &included, true)
	if err != nil {
		return nil, err
	}
	payload := &OnePayload{Data: rootNode}

	payload.Included = nodeMapValues(&included)

	return payload, nil
}

func AddIncludedToOnePayload(payload *OnePayload, node *Node) {

	payload.Included = append(payload.Included, node)
}

func AddIncludedToManyPayload(payload *ManyPayload, node *Node) {

	payload.Included = append(payload.Included, node)
}

// MarshalManyPayloadWithoutIncluded writes a jsonapi response with many records,
// without the related records sideloaded into "included" array. If you want to
// serialize the relations into the "included" array see MarshalManyPayload.
//
// models interface{} should be a slice of struct pointers.
func MarshalManyPayloadWithoutIncluded(w io.Writer, models interface{}) error {
	m, err := convertToSliceInterface(&models)
	if err != nil {
		return err
	}
	payload, err := MarshalMany(m)
	if err != nil {
		return err
	}

	// Empty the included
	payload.Included = []*Node{}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return err
	}

	return nil
}

// MarshalManyPayload writes a jsonapi response with many records, with related
// records sideloaded, into "included" array. This method encodes a response for
// a slice of records, hence data will be an array of records rather than a
// single record.  To serialize a single record, see MarshalOnePayload
//
// For example you could pass it, w, your http.ResponseWriter, and, models, a
// slice of Blog struct instance pointers as interface{}'s to write to the
// response,
//
//	 func ListBlogs(w http.ResponseWriter, r *http.Request) {
//		 // ... fetch your blogs and filter, offset, limit, etc ...
//
//		 blogs := testBlogsForList()
//
//		 w.Header().Set("Content-Type", jsonapi.MediaType)
//		 w.WriteHeader(http.StatusOK)
//
//		 if err := jsonapi.MarshalManyPayload(w, blogs); err != nil {
//			 http.Error(w, err.Error(), http.StatusInternalServerError)
//		 }
//	 }
//
//
// Visit https://github.com/google/jsonapi#list for more info.
//
// models interface{} should be a slice of struct pointers.
func MarshalManyPayload(w io.Writer, models interface{}) error {
	m, err := convertToSliceInterface(&models)
	if err != nil {
		return err
	}
	payload, err := MarshalMany(m)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return err
	}

	return nil
}

// MarshalMany does the same as MarshalManyPayload except it just returns the
// payload and doesn't write out results. Useful is you use your JSON rendering
// library.
func MarshalMany(models []interface{}) (*ManyPayload, error) {
	payload := &ManyPayload{
		Data: []*Node{},
	}
	included := map[string]*Node{}

	for _, model := range models {
		node, err := VisitModelNode(model, &included, true)
		if err != nil {
			return nil, err
		}
		payload.Data = append(payload.Data, node)
	}
	payload.Included = nodeMapValues(&included)

	return payload, nil
}

// MarshalOnePayloadEmbedded - This method not meant to for use in
// implementation code, although feel free.  The purpose of this method is for
// use in tests.  In most cases, your request payloads for create will be
// embedded rather than sideloaded for related records. This method will
// serialize a single struct pointer into an embedded json response.  In other
// words, there will be no, "included", array in the json all relationships will
// be serailized inline in the data.
//
// However, in tests, you may want to construct payloads to post to create
// methods that are embedded to most closely resemble the payloads that will be
// produced by the client.  This is what this method is intended for.
//
// model interface{} should be a pointer to a struct.
func MarshalOnePayloadEmbedded(w io.Writer, model interface{}) error {
	rootNode, err := VisitModelNode(model, nil, false)
	if err != nil {
		return err
	}

	payload := &OnePayload{Data: rootNode}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return err
	}

	return nil
}

func VisitModelNode(model interface{}, included *map[string]*Node,
	sideload bool) (*Node, error) {
	node := new(Node)

	var er error

	modelValue := reflect.ValueOf(model).Elem()
	modelType := reflect.ValueOf(model).Type().Elem()

	for i := 0; i < modelValue.NumField(); i++ {
		structField := modelValue.Type().Field(i)
		tag := structField.Tag.Get(annotationJSONAPI)
		if tag == "" {
			continue
		}

		fieldValue := modelValue.Field(i)
		fieldType := modelType.Field(i)

		args := strings.Split(tag, annotationSeperator)

		if len(args) < 1 {
			er = ErrBadJSONAPIStructTag
			break
		}

		annotation := args[0]

		if (annotation == annotationClientID && len(args) != 1) ||
			(annotation != annotationClientID && len(args) < 2) {
			panic("weird args for model")
			er = ErrBadJSONAPIStructTag
			break
		}

		if annotation == annotationPrimary {
			v := fieldValue

			// Deal with PTRS
			var kind reflect.Kind
			if fieldValue.Kind() == reflect.Ptr {
				kind = fieldType.Type.Elem().Kind()
				v = reflect.Indirect(fieldValue)
			} else {
				kind = fieldType.Type.Kind()
			}

			// Handle allowed types
			switch kind {
			case reflect.String:
				node.ID = v.Interface().(string)
			case reflect.Int:
				node.ID = strconv.FormatInt(int64(v.Interface().(int)), 10)
			case reflect.Int8:
				node.ID = strconv.FormatInt(int64(v.Interface().(int8)), 10)
			case reflect.Int16:
				node.ID = strconv.FormatInt(int64(v.Interface().(int16)), 10)
			case reflect.Int32:
				node.ID = strconv.FormatInt(int64(v.Interface().(int32)), 10)
			case reflect.Int64:
				node.ID = strconv.FormatInt(v.Interface().(int64), 10)
			case reflect.Uint:
				node.ID = strconv.FormatUint(uint64(v.Interface().(uint)), 10)
			case reflect.Uint8:
				node.ID = strconv.FormatUint(uint64(v.Interface().(uint8)), 10)
			case reflect.Uint16:
				node.ID = strconv.FormatUint(uint64(v.Interface().(uint16)), 10)
			case reflect.Uint32:
				node.ID = strconv.FormatUint(uint64(v.Interface().(uint32)), 10)
			case reflect.Uint64:
				node.ID = strconv.FormatUint(v.Interface().(uint64), 10)
			default:
				// We had a JSON float (numeric), but our field was not one of the
				// allowed numeric types
				er = ErrBadJSONAPIID
				break
			}

			node.Type = args[1]
		} else if annotation == annotationClientID {
			clientID := fieldValue.String()
			if clientID != "" {
				node.ClientID = clientID
			}
		} else if annotation == annotationAttribute {
			var omitEmpty, iso8601 bool

			if len(args) > 2 {
				for _, arg := range args[2:] {
					switch arg {
					case annotationOmitEmpty:
						omitEmpty = true
					case annotationISO8601:
						iso8601 = true
					}
				}
			}

			if node.Attributes == nil {
				node.Attributes = make(map[string]interface{})
			}

			if fieldValue.Type() == reflect.TypeOf(time.Time{}) {
				t := fieldValue.Interface().(time.Time)

				if t.IsZero() {
					continue
				}

				if iso8601 {
					node.Attributes[args[1]] = t.UTC().Format(iso8601TimeFormat)
				} else {
					node.Attributes[args[1]] = t.Unix()
				}
			} else if fieldValue.Type() == reflect.TypeOf(new(time.Time)) {
				// A time pointer may be nil
				if fieldValue.IsNil() {
					if omitEmpty {
						continue
					}

					node.Attributes[args[1]] = nil
				} else {
					tm := fieldValue.Interface().(*time.Time)

					if tm.IsZero() && omitEmpty {
						continue
					}

					if iso8601 {
						node.Attributes[args[1]] = tm.UTC().Format(iso8601TimeFormat)
					} else {
						node.Attributes[args[1]] = tm.Unix()
					}
				}
			} else {
				// Dealing with a fieldValue that is not a time
				emptyValue := reflect.Zero(fieldValue.Type())

				// See if we need to omit this field
				if omitEmpty && fieldValue.Interface() == emptyValue.Interface() {
					continue
				}

				strAttr, ok := fieldValue.Interface().(string)
				if ok {
					node.Attributes[args[1]] = strAttr
				} else {
					node.Attributes[args[1]] = fieldValue.Interface()
				}
			}
		} else if annotation == annotationRelation {
			var omitEmpty bool

			//add support for 'omitempty' struct tag for marshaling as absent
			if len(args) > 2 {
				omitEmpty = args[2] == annotationOmitEmpty
			}

			isSlice := fieldValue.Type().Kind() == reflect.Slice
			if omitEmpty &&
				(isSlice && fieldValue.Len() < 1 ||
					(!isSlice && fieldValue.IsNil())) {
				continue
			}

			if node.Relationships == nil {
				node.Relationships = make(map[string]interface{})
			}

			var relLinks *Links
			if linkableModel, ok := model.(RelationshipLinkable); ok {
				relLinks = linkableModel.JSONAPIRelationshipLinks(args[1])
			}

			var relMeta *Meta
			if metableModel, ok := model.(RelationshipMetable); ok {
				relMeta = metableModel.JSONAPIRelationshipMeta(args[1])
			}

			if isSlice {
				// to-many relationship
				relationship, err := visitModelNodeRelationships(
					fieldValue,
					included,
					sideload,
				)
				if err != nil {
					er = err
					break
				}
				relationship.Links = relLinks
				relationship.Meta = relMeta

				if sideload {
					shallowNodes := []*Node{}
					for _, n := range relationship.Data {
						appendIncluded(included, n)
						shallowNodes = append(shallowNodes, toShallowNode(n))
					}

					node.Relationships[args[1]] = &RelationshipManyNode{
						Data:  shallowNodes,
						Links: relationship.Links,
						Meta:  relationship.Meta,
					}
				} else {
					node.Relationships[args[1]] = relationship
				}
			} else {
				// to-one relationships

				// Handle null relationship case
				if fieldValue.IsNil() {
					node.Relationships[args[1]] = &RelationshipOneNode{Data: nil}
					continue
				}

				relationship, err := VisitModelNode(
					fieldValue.Interface(),
					included,
					sideload,
				)
				if err != nil {
					er = err
					break
				}

				if sideload {
					appendIncluded(included, relationship)
					node.Relationships[args[1]] = &RelationshipOneNode{
						Data:  toShallowNode(relationship),
						Links: relLinks,
						Meta:  relMeta,
					}
				} else {
					node.Relationships[args[1]] = &RelationshipOneNode{
						Data:  relationship,
						Links: relLinks,
						Meta:  relMeta,
					}
				}
			}

		} else {
			er = ErrBadJSONAPIStructTag
			break
		}
	}

	if er != nil {
		return nil, er
	}

	if linkableModel, isLinkable := model.(Linkable); isLinkable {
		jl := linkableModel.JSONAPILinks()
		if er := jl.validate(); er != nil {
			return nil, er
		}
		node.Links = linkableModel.JSONAPILinks()
	}

	if metableModel, ok := model.(Metable); ok {
		node.Meta = metableModel.JSONAPIMeta()
	}

	return node, nil
}

func toShallowNode(node *Node) *Node {
	return &Node{
		ID:   node.ID,
		Type: node.Type,
	}
}

func visitModelNodeRelationships(models reflect.Value, included *map[string]*Node,
	sideload bool) (*RelationshipManyNode, error) {
	nodes := []*Node{}

	for i := 0; i < models.Len(); i++ {
		n := models.Index(i).Interface()

		node, err := VisitModelNode(n, included, sideload)
		if err != nil {
			return nil, err
		}

		nodes = append(nodes, node)
	}

	return &RelationshipManyNode{Data: nodes}, nil
}

func appendIncluded(m *map[string]*Node, nodes ...*Node) {
	included := *m

	for _, n := range nodes {
		k := fmt.Sprintf("%s,%s", n.Type, n.ID)

		if _, hasNode := included[k]; hasNode {
			continue
		}

		included[k] = n
	}
}

func nodeMapValues(m *map[string]*Node) []*Node {
	mp := *m
	nodes := make([]*Node, len(mp))

	i := 0
	for _, n := range mp {
		nodes[i] = n
		i++
	}

	return nodes
}

func convertToSliceInterface(i *interface{}) ([]interface{}, error) {
	vals := reflect.ValueOf(*i)
	if vals.Kind() != reflect.Slice {
		return nil, ErrExpectedSlice
	}
	var response []interface{}
	for x := 0; x < vals.Len(); x++ {
		response = append(response, vals.Index(x).Interface())
	}
	return response, nil
}
