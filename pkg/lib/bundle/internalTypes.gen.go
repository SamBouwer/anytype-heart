/*
Code generated by pkg/lib/bundle/generator. DO NOT EDIT.
source: pkg/lib/bundle/internalTypes.json
*/
package bundle

const InternalTypesChecksum = "d80636e9d0d0b6e96ecd0bd7e1dd8279298778ed077fe748c4c129de6a9128f1"

// InternalTypes contains the list of types that are not possible to create directly via ObjectCreate
// to create as a general object because they have specific logic
var InternalTypes = []TypeKey{
	TypeKeyFile,
	TypeKeyImage,
	TypeKeyVideo,
	TypeKeyAudio,
	TypeKeySpace,
	TypeKeyDashboard,
	TypeKeyObjectType,
	TypeKeyRelation,
	TypeKeyRelationOption,
	TypeKeyDate,
	TypeKeyTemplate,
}
