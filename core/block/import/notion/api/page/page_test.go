package page

import (
	"context"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/assert"

	"github.com/anyproto/anytype-heart/core/block/import/notion/api"
	"github.com/anyproto/anytype-heart/core/block/import/notion/api/block"
	"github.com/anyproto/anytype-heart/core/block/import/notion/api/client"
	"github.com/anyproto/anytype-heart/core/block/import/notion/api/property"
	"github.com/anyproto/anytype-heart/util/pbtypes"
)

func Test_handlePagePropertiesSelect(t *testing.T) {
	details := make(map[string]*types.Value, 0)
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	p := property.SelectItem{
		Object: "",
		ID:     "id",
		Type:   string(property.PropertyConfigTypeSelect),
		Select: property.SelectOption{
			ID:    "id",
			Name:  "Name",
			Color: api.Blue,
		},
	}
	pr := property.Properties{"Select": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 2) // 1 relation + 1 option
	assert.Len(t, req.RelationsIdsToOptions, 1)
	for _, options := range req.RelationsIdsToOptions {
		assert.Len(t, options, 1)
		assert.NotNil(t, options[0].Details)
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Name"))
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("blue"))
	}

	//Relation already exist
	p = property.SelectItem{
		Object: "",
		ID:     "id",
		Type:   string(property.PropertyConfigTypeSelect),
		Select: property.SelectOption{
			ID:    "id",
			Name:  "Name 2",
			Color: api.Pink,
		},
	}
	snapshots, _ = ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.NotEmpty(t, req)
	assert.Len(t, snapshots, 1) // 1 option
	assert.Len(t, req.RelationsIdsToOptions, 1)
	for _, options := range req.RelationsIdsToOptions {
		assert.Len(t, options, 2)
		assert.NotNil(t, options[0].Details)
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Name"))
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("blue"))

		assert.NotNil(t, options[1].Details)
		assert.Equal(t, options[1].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Name 2"))
		assert.Equal(t, options[1].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("pink"))
	}
}

func Test_handlePagePropertiesLastEditedTime(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.LastEditedTimeItem{
		ID:             "id",
		Type:           string(property.PropertyConfigLastEditedTime),
		LastEditedTime: "2022-10-24T22:56:00.000Z",
	}
	pr := property.Properties{"LastEditedTime": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 1) // 1 relation
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])
}

func Test_handlePagePropertiesRichText(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"list","results":[{"object":"property_item","type":"rich_text","id":"RPBv","rich_text":{"type":"text","text":{"content":"sdfsdfsdfsdfsdfsdf","link":null},"annotations":{"bold":false,"italic":false,"strikethrough":false,"underline":false,"code":false,"color":"default"},"plain_text":"example text","href":null}}],"next_cursor":null,"has_more":false,"type":"property_item","property_item":{"id":"RPBv","next_url":null,"type":"rich_text","rich_text":{}}}`))
	}))

	c := client.NewClient()
	c.BasePath = s.URL
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.RichTextItem{ID: "id", Type: string(property.PropertyConfigTypeRichText)}
	pr := property.Properties{"RichText": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 1) // 1 relation
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])
}

func Test_handlePagePropertiesStatus(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.StatusItem{
		ID:   "id",
		Type: property.PropertyConfigStatus,
		Status: &property.Status{
			Name:  "Done",
			ID:    "id",
			Color: api.Pink,
		},
	}
	pr := property.Properties{"Status": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 2) // 1 relation + 1 option
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])

	assert.Len(t, req.RelationsIdsToOptions, 1)
	for _, options := range req.RelationsIdsToOptions {
		assert.Len(t, options, 1)
		assert.NotNil(t, options[0].Details)
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Done"))
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("pink"))
	}

	//Relation already exist
	p = property.StatusItem{
		ID:   "id",
		Type: property.PropertyConfigStatus,
		Status: &property.Status{
			Name:  "In progress",
			ID:    "id",
			Color: api.Gray,
		},
	}
	snapshots, _ = ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.NotEmpty(t, req)
	assert.Len(t, snapshots, 1) // 1 option
	assert.Len(t, req.RelationsIdsToOptions, 1)
	for _, options := range req.RelationsIdsToOptions {
		assert.Len(t, options, 2)
		assert.NotNil(t, options[0].Details)
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Done"))
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("pink"))

		assert.NotNil(t, options[1].Details)
		assert.Equal(t, options[1].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("In progress"))
		assert.Equal(t, options[1].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("grey"))
	}
}

func Test_handlePagePropertiesNumber(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	num := float64(12)
	p := property.NumberItem{
		ID:     "id",
		Type:   string(property.PropertyConfigTypeNumber),
		Number: &num,
	}
	pr := property.Properties{"Number": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 1) // 1 relation
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])
}

func Test_handlePagePropertiesMultiSelect(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.MultiSelectItem{
		ID:   "id",
		Type: string(property.PropertyConfigTypeMultiSelect),
		MultiSelect: []*property.SelectOption{
			{
				ID:    "id",
				Name:  "Name",
				Color: api.Blue,
			},
		},
	}
	pr := property.Properties{"MultiSelect": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 2) // 1 relation + 1 option
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])

	assert.Len(t, req.RelationsIdsToOptions, 1)
	for _, options := range req.RelationsIdsToOptions {
		assert.Len(t, options, 1)
		assert.NotNil(t, options[0].Details)
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Name"))
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("blue"))
	}

	//Relation already exist
	p = property.MultiSelectItem{
		ID:   "id",
		Type: string(property.PropertyConfigTypeMultiSelect),
		MultiSelect: []*property.SelectOption{
			{
				ID:    "id",
				Name:  "Name 2",
				Color: api.Purple,
			},
		},
	}
	snapshots, _ = ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.NotEmpty(t, req)
	assert.Len(t, snapshots, 1) // 1 option
	assert.Len(t, req.RelationsIdsToOptions, 1)
	for _, options := range req.RelationsIdsToOptions {
		assert.Len(t, options, 2)
		assert.NotNil(t, options[0].Details)
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Name"))
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("blue"))

		assert.NotNil(t, options[1].Details)
		assert.Equal(t, options[1].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Name 2"))
		assert.Equal(t, options[1].Details.Fields[bundle.RelationKeyRelationOptionColor.String()], pbtypes.String("purple"))
	}
}

func Test_handlePagePropertiesCheckbox(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.CheckboxItem{
		ID:       "id",
		Type:     string(property.PropertyConfigTypeCheckbox),
		Checkbox: true,
	}
	pr := property.Properties{"Checkbox": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 1) // 1 relation
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])
}

func Test_handlePagePropertiesEmail(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	email := "a@mail.com"
	p := property.EmailItem{
		ID:    "id",
		Type:  string(property.PropertyConfigTypeEmail),
		Email: &email,
	}
	pr := property.Properties{"Email": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 1) // 1 relation
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])
}

func Test_handlePagePropertiesRelation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"list","results":[{"object":"property_item","type":"relation","id":"cm~~","relation":{"id":"id"}}],"next_cursor":null,"has_more":false,"type":"property_item","property_item":{"id":"cm~~","next_url":null,"type":"relation","relation":{}}}`))
	}))

	c := client.NewClient()
	c.BasePath = s.URL
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.RelationItem{ID: "id", Type: string(property.PropertyConfigTypeRelation), HasMore: true, Relation: []*property.Relation{{ID: "id"}}}
	pr := property.Properties{"Relation": &p}
	notionPageIdsToAnytype := map[string]string{"id": "anytypeID"}
	notionDatabaseIdsToAnytype := map[string]string{"id": "anytypeID"}
	req := &block.MapRequest{
		NotionPageIdsToAnytype:     notionPageIdsToAnytype,
		NotionDatabaseIdsToAnytype: notionDatabaseIdsToAnytype,
		RelationsIdsToAnytypeID:    map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:      map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 1) // 1 relation
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key].GetListValue())
	assert.Len(t, details[key].GetListValue().Values, 1)
	assert.Equal(t, pbtypes.GetStringListValue(details[key])[0], "anytypeID")
}

func Test_handlePagePropertiesPeople(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"list","results":[{"object":"property_item","type":"people","id":"id","people":{"object":"user","id":"1","name":"Example","avatar_url":"https://example1.com","type":"person","person":{"email":"email1@.com"}}},{"object":"property_item","type":"people","id":"id","people":{"object":"user","id":"2","name":"Example 2","avatar_url":"https://example2.com","type":"person","person":{"email":"email2@.com"}}}],"next_cursor":null,"has_more":false,"type":"property_item","property_item":{"id":"id","next_url":null,"type":"people","people":{}}}`))
	}))
	c := client.NewClient()
	c.BasePath = s.URL
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.PeopleItem{
		Object: "",
		ID:     "id",
		Type:   string(property.PropertyConfigTypePeople),
	}
	pr := property.Properties{"People": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 3) // 1 relation + 1 option
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])

	for _, options := range req.RelationsIdsToOptions {
		assert.Len(t, options, 2)
		assert.NotNil(t, options[0].Details)
		assert.Equal(t, options[0].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Example"))

		assert.NotNil(t, options[1].Details)
		assert.Equal(t, options[1].Details.Fields[bundle.RelationKeyName.String()], pbtypes.String("Example 2"))
	}
}

func Test_handlePagePropertiesFormula(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.FormulaItem{
		ID:      "id",
		Type:    string(property.PropertyConfigTypeFormula),
		Formula: map[string]interface{}{"type": property.NumberFormula, "number": float64(1)},
	}
	pr := property.Properties{"Formula": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 1) // 1 relation
	assert.Len(t, req.RelationsIdsToAnytypeID, 1)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id"].Details, bundle.RelationKeyRelationKey.String())
	assert.NotEmpty(t, details[key])
}

func Test_handlePagePropertiesTitle(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p := property.TitleItem{
		ID:    "id",
		Type:  string(property.PropertyConfigTypeTitle),
		Title: []*api.RichText{{PlainText: "Title"}},
	}
	pr := property.Properties{"Title": &p}
	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)
	assert.Len(t, snapshots, 0) // not create snapshot for existing anytype relation name
}

func Test_handleRollupProperties(t *testing.T) {
	c := client.NewClient()
	ps := Task{
		propertyService:        property.New(c),
		relationOptCreateMutex: &sync.Mutex{},
		relationCreateMutex:    &sync.Mutex{},
	}

	details := make(map[string]*types.Value, 0)

	p1 := property.RollupItem{
		ID:   "id1",
		Type: string(property.PropertyConfigTypeRollup),
		Rollup: property.RollupObject{
			Type:   "number",
			Number: 2,
		},
	}

	p2 := property.RollupItem{
		ID:   "id2",
		Type: string(property.PropertyConfigTypeRollup),
		Rollup: property.RollupObject{
			Type: "date",
			Date: &api.DateObject{
				Start: "12-12-2022",
			},
		},
	}

	p3 := property.RollupItem{
		ID:   "id3",
		Type: string(property.PropertyConfigTypeRollup),
		Rollup: property.RollupObject{
			Type: "array",
			Array: []interface{}{
				map[string]interface{}{"type": "title", "title": []map[string]string{{"plain_text": "Title"}}},
			},
		},
	}

	pr := property.Properties{"Rollup1": &p1, "Rollup2": &p2, "Rollup3": &p3}

	req := &block.MapRequest{
		RelationsIdsToAnytypeID: map[string]*model.SmartBlockSnapshotBase{},
		RelationsIdsToOptions:   map[string][]*model.SmartBlockSnapshotBase{},
	}
	snapshots, _ := ps.handlePageProperties(context.TODO(), "key", "id", pr, details, req)

	assert.Len(t, snapshots, 3) // 3 relations
	assert.Len(t, req.RelationsIdsToAnytypeID, 3)
	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id1"])
	key := pbtypes.GetString(req.RelationsIdsToAnytypeID["id1"].Details, bundle.RelationKeyRelationKey.String())
	assert.Equal(t, details[key].GetNumberValue(), float64(2))

	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id2"])
	key = pbtypes.GetString(req.RelationsIdsToAnytypeID["id2"].Details, bundle.RelationKeyRelationKey.String())
	assert.Equal(t, details[key].GetStringValue(), "12-12-2022")

	assert.NotEmpty(t, req.RelationsIdsToAnytypeID["id3"])
	key = pbtypes.GetString(req.RelationsIdsToAnytypeID["id3"].Details, bundle.RelationKeyRelationKey.String())
	assert.Len(t, pbtypes.GetStringListValue(details[key]), 1)
	rollup := pbtypes.GetStringListValue(details[key])
	assert.Equal(t, rollup[0], "Title")
}
