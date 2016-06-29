package sdk

import (
	"fmt"
	"github.com/connctd/sdk-go/protocol"
	"gopkg.in/yaml.v2"
	"strings"
)

type ValueType byte
type ThingStatus byte

const (
	Boolean ValueType = iota
	String  ValueType = iota
	Number  ValueType = iota
)

var (
	ValueTypeStrings = []string{
		"BOOLEAN",
		"STRING",
		"NUMBER",
	}

	ThingStatusStrings = []string{
		"Unknown",
	}
)

const (
	Unknown ThingStatus = iota
)

func (v ValueType) Protocol() *protocol.ValueType {
	return protocolValueTypeFromValueType(v)
}

func (t ThingStatus) Protocol() *protocol.ThingStatus {
	status := protocol.ThingStatus(t + 1)
	return &status
}

type Value struct {
	Type   ValueType
	Symbol string
	Value  string `yaml:",omitempty"`
}

func (v *Value) Protocol() *protocol.Value {
	return &protocol.Value{
		ValueType: protocolValueTypeFromValueType(v.Type),
		Symbol:    &v.Symbol,
		Value:     &v.Value,
	}
}

type Property struct {
	Value  *Value
	Name   string
	client *Client
	parent *Capability
}

func (p *Property) Protocol() *protocol.Property {
	return &protocol.Property{
		Value: p.Value.Protocol(),
		Name:  &p.Name,
	}
}

func (p *Property) Update(newValue string) error {
	// Only update if value has changed
	if p.Value.Value != newValue {
		path := &protocol.Path{
			Property:    &p.Name,
			ComponentId: &p.parent.parent.Id,
			ThingId:     &p.parent.parent.parent.Id,
		}
		value := &protocol.Value{
			Value:     &newValue,
			ValueType: protocolValueTypeFromValueType(p.Value.Type),
			Symbol:    &p.Value.Symbol,
		}
		propertyChange := &protocol.ClientMessage_PropertyChange{
			Path:  path,
			Value: value,
		}
		cm := &protocol.ClientMessage{
			PropertyChange: propertyChange,
		}
		p.client.send(cm)
	}
	return nil
}

func protocolValueTypeFromValueType(v ValueType) *protocol.ValueType {
	vt := protocol.ValueType(protocol.ValueType_value[strings.ToUpper(v.String())])
	return &vt
}

type ActionParameter struct {
	Type *ValueType
	Name string
}

func (a *ActionParameter) Protocol() *protocol.Action_Parameter {
	return &protocol.Action_Parameter{
		ValueType: a.Type.Protocol(),
		Name:      &a.Name,
	}
}

type Action struct {
	Name       string
	Parameters []*ActionParameter                         `yaml:",omitempty"`
	Execute    func(action Action, params []string) error `yaml:"-"`
	parent     *Capability
}

func (a *Action) Protocol() *protocol.Action {
	params := make([]*protocol.Action_Parameter, 0, len(a.Parameters))
	for _, p := range a.Parameters {
		params = append(params, p.Protocol())
	}
	return &protocol.Action{
		Parameters: params,
		Name:       &a.Name,
	}
}

type Capability struct {
	Id         string
	Actions    []*Action
	Properties []*Property
	parent     *Component
}

func (c *Capability) Protocol() *protocol.Capability {
	actions := make([]*protocol.Action, 0, len(c.Actions))
	for _, action := range c.Actions {
		actions = append(actions, action.Protocol())
	}

	properties := make([]*protocol.Property, 0, len(c.Properties))
	for _, property := range c.Properties {
		properties = append(properties, property.Protocol())
	}
	return &protocol.Capability{
		Actions:    actions,
		Properties: properties,
		Id:         &c.Id,
	}
}

type Component struct {
	Id            string
	Name          string
	ComponentType string
	Capabilities  []*Capability
	Properties    []*Property `yaml:",omitempty"`
	Actions       []*Action   `yaml:",omitempty"`
	parent        *Thing
}

func (c *Component) Protocol() *protocol.Component {
	capabilities := make([]*protocol.Capability, 0, len(c.Capabilities))
	properties := make([]*protocol.Property, 0, len(c.Properties))
	actions := make([]*protocol.Action, 0, len(c.Actions))

	for _, capability := range c.Capabilities {
		capabilities = append(capabilities, capability.Protocol())
	}

	for _, property := range c.Properties {
		properties = append(properties, property.Protocol())
	}

	for _, action := range c.Actions {
		actions = append(actions, action.Protocol())
	}
	return &protocol.Component{
		Name:          &c.Name,
		Id:            &c.Id,
		Capabilities:  capabilities,
		Properties:    properties,
		Actions:       actions,
		ComponentType: &c.ComponentType,
	}
}

func (c *Component) GetAction(actionName string) *Action {
	for _, capability := range c.Capabilities {
		for _, action := range capability.Actions {
			if action.Name == actionName {
				return action
			}
		}
	}
	return nil
}

type Attribute struct {
	Name  string
	Value string
}

func (a *Attribute) Protocol() *protocol.Thing_Attribute {
	return &protocol.Thing_Attribute{
		Name:  &a.Name,
		Value: &a.Value,
	}
}

type Thing struct {
	Components      []*Component
	Id              string
	Name            string
	Manufacturer    string
	DisplayType     string
	MaincomponentId string
	Attributes      []*Attribute `yaml:",omitempty"`
	ComponentType   string
}

func (v ValueType) String() string {
	return ValueTypeStrings[v]
}

func ValueTypeFromString(s string) (ValueType, error) {
	for i, valueType := range ValueTypeStrings {
		if valueType == s {
			return ValueType(i), nil
		}
	}
	return String, fmt.Errorf("Unknwon ValueType: %s", s)
}

func (v ValueType) MarshalYAML() (interface{}, error) {
	return v.String(), nil
}

func (v ValueType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var valueTypeString string
	err := unmarshal(&valueTypeString)
	if err != nil {
		return err
	}
	found := false
	for i, name := range ValueTypeStrings {
		if name == valueTypeString {
			v = ValueType(i)
			found = true
		}
	}
	if !found {
		return fmt.Errorf("Invalid ValueType")
	}
	return nil
}

func (t *Thing) String() string {
	bytes, _ := yaml.Marshal(t)
	return string(bytes)
}

func (t *Thing) Protocol() *protocol.Thing {
	attributes := make([]*protocol.Thing_Attribute, 0, len(t.Attributes))
	components := make([]*protocol.Component, 0, len(t.Components))

	for _, attribute := range t.Attributes {
		attributes = append(attributes, attribute.Protocol())
	}

	for _, component := range t.Components {
		components = append(components, component.Protocol())
	}
	thing := &protocol.Thing{
		Id:              &t.Id,
		Name:            &t.Name,
		Manufacturer:    &t.Manufacturer,
		DisplayType:     &t.DisplayType,
		MaincomponentId: &t.MaincomponentId,
		Attributes:      attributes,
		Components:      components,
	}
	return thing
}

func (t *Thing) GetComponent(componentId string) *Component {
	for _, component := range t.Components {
		if componentId == component.Id {
			return component
		}
	}
	return nil
}
