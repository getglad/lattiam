package deployment

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/mitchellh/mapstructure"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// UpdatePlanSerializer handles safe serialization and deserialization of update plans
type UpdatePlanSerializer struct{}

// NewUpdatePlanSerializer creates a new update plan serializer
func NewUpdatePlanSerializer() *UpdatePlanSerializer {
	return &UpdatePlanSerializer{}
}

// Serialize converts an UpdatePlan to a map for JSON serialization
func (s *UpdatePlanSerializer) Serialize(plan *interfaces.UpdatePlan) (map[string]interface{}, error) {
	if plan == nil {
		return nil, fmt.Errorf("update plan is nil")
	}

	// Use JSON marshal/unmarshal for safe conversion
	data, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update plan: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to map: %w", err)
	}

	return result, nil
}

// Deserialize converts various types to UpdatePlan using mapstructure
func (s *UpdatePlanSerializer) Deserialize(input interface{}) (*interfaces.UpdatePlan, error) {
	if input == nil {
		return nil, fmt.Errorf("input is nil")
	}

	// If already an UpdatePlan, return it
	if plan, ok := input.(*interfaces.UpdatePlan); ok {
		return plan, nil
	}

	// Create decoder with custom settings
	var plan interfaces.UpdatePlan
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           &plan,
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			s.stringToChangeActionHook(),
			s.stringToResourceKeyHook(),
		),
		TagName: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create decoder: %w", err)
	}

	if err := decoder.Decode(input); err != nil {
		return nil, fmt.Errorf("failed to decode update plan: %w", err)
	}

	// Validate the deserialized plan
	if err := s.validatePlan(&plan); err != nil {
		return nil, fmt.Errorf("invalid update plan: %w", err)
	}

	return &plan, nil
}

// stringToChangeActionHook converts string to ChangeAction
func (s *UpdatePlanSerializer) stringToChangeActionHook() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		if t != reflect.TypeOf(interfaces.ActionNoOp) {
			return data, nil
		}

		str, ok := data.(string)
		if !ok {
			return data, nil
		}

		switch str {
		case "create":
			return interfaces.ActionCreate, nil
		case "update":
			return interfaces.ActionUpdate, nil
		case "delete":
			return interfaces.ActionDelete, nil
		case "replace":
			return interfaces.ActionReplace, nil
		case "noop", "no-op":
			return interfaces.ActionNoOp, nil
		default:
			return nil, fmt.Errorf("unknown action: %s", str)
		}
	}
}

// stringToResourceKeyHook converts string to ResourceKey
func (s *UpdatePlanSerializer) stringToResourceKeyHook() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		if t != reflect.TypeOf(interfaces.ResourceKey("")) {
			return data, nil
		}

		str, ok := data.(string)
		if !ok {
			return data, nil
		}

		return interfaces.ResourceKey(str), nil
	}
}

// validatePlan ensures the plan has all required fields
func (s *UpdatePlanSerializer) validatePlan(plan *interfaces.UpdatePlan) error { //nolint:gocognit,gocyclo // Comprehensive plan validation
	if plan.DeploymentID == "" {
		return fmt.Errorf("deployment ID is required")
	}
	if plan.PlanID == "" {
		return fmt.Errorf("plan ID is required")
	}
	if len(plan.Changes) == 0 {
		return fmt.Errorf("no changes in update plan")
	}

	// Validate each change
	for i, change := range plan.Changes {
		if change.ResourceKey == "" {
			return fmt.Errorf("change %d: resource key is required", i)
		}
		if change.Action == "" {
			return fmt.Errorf("change %d: action is required", i)
		}

		// Validate action-specific requirements
		switch change.Action {
		case interfaces.ActionCreate:
			if change.After == nil {
				return fmt.Errorf("change %d: 'after' state required for create action", i)
			}
		case interfaces.ActionUpdate:
			if change.Before == nil || change.After == nil {
				return fmt.Errorf("change %d: both 'before' and 'after' states required for update action", i)
			}
		case interfaces.ActionDelete:
			if change.Before == nil {
				return fmt.Errorf("change %d: 'before' state required for delete action", i)
			}
		case interfaces.ActionNoOp:
			// No specific validation needed for no-op changes
		case interfaces.ActionReplace:
			if change.Before == nil || change.After == nil {
				return fmt.Errorf("change %d: both 'before' and 'after' states required for replace action", i)
			}
		}
	}

	return nil
}

// ExtractUpdatePlan safely extracts and deserializes an update plan from deployment metadata
func ExtractUpdatePlan(deployment *interfaces.QueuedDeployment) (*interfaces.UpdatePlan, error) {
	if deployment == nil {
		return nil, fmt.Errorf("deployment is nil")
	}
	if deployment.Request == nil {
		return nil, fmt.Errorf("deployment request is nil")
	}
	if deployment.Request.Metadata == nil {
		return nil, fmt.Errorf("deployment metadata is nil")
	}

	planInterface, ok := deployment.Request.Metadata[interfaces.MetadataKeyUpdatePlan]
	if !ok {
		return nil, fmt.Errorf("update_plan not found in deployment metadata")
	}

	serializer := NewUpdatePlanSerializer()
	return serializer.Deserialize(planInterface)
}
