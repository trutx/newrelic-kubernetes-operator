/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"context"
	"errors"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/newrelic/newrelic-kubernetes-operator/interfaces"
)

// log is for logging in this package.
var (
	log = logf.Log.WithName("nrqlalertcondition-resource")
)

func (r *NrqlAlertCondition) SetupWebhookWithManager(mgr ctrl.Manager) error {
	alertClientFunc = interfaces.InitializeAlertsClient
	k8Client = mgr.GetClient()
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-nr-k8s-newrelic-com-v1-nrqlalertcondition,mutating=true,failurePolicy=fail,groups=nr.k8s.newrelic.com,resources=nrqlalertconditions,verbs=create;update,versions=v1,name=mnrqlalertcondition.kb.io,sideEffects=None

var _ webhook.Defaulter = &NrqlAlertCondition{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *NrqlAlertCondition) Default() {
	log.Info("default", "name", r.Name)

	if r.Status.AppliedSpec == nil {
		log.Info("Setting null Applied Spec to empty interface")
		r.Status.AppliedSpec = &NrqlAlertConditionSpec{}
	}
	log.Info("r.Status.AppliedSpec after", "r.Status.AppliedSpec", r.Status.AppliedSpec)
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-nr-k8s-newrelic-com-v1-nrqlalertcondition,mutating=false,failurePolicy=fail,groups=nr.k8s.newrelic.com,resources=nrqlalertconditions,versions=v1,name=vnrqlalertcondition.kb.io,sideEffects=None

var _ webhook.Validator = &NrqlAlertCondition{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *NrqlAlertCondition) ValidateCreate() error {
	log.Info("validate create", "name", r.Name)
	//TODO this should write this value TO a new secret so code path always reads from a secret
	err := r.CheckForAPIKeyOrSecret()
	if err != nil {
		return err
	}

	err = r.CheckRequiredFields()
	if err != nil {
		return err
	}

	return r.CheckExistingPolicyID()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *NrqlAlertCondition) ValidateUpdate(old runtime.Object) error {
	log.Info("validate update", "name", r.Name)
	err := r.CheckForAPIKeyOrSecret()
	if err != nil {
		return err
	}

	err = r.CheckRequiredFields()
	if err != nil {
		return err
	}

	return r.CheckExistingPolicyID()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *NrqlAlertCondition) ValidateDelete() error {
	log.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

func (r *NrqlAlertCondition) CheckExistingPolicyID() error {
	log.Info("Checking existing", "policyId", r.Spec.ExistingPolicyID)
	ctx := context.Background()
	var apiKey string

	if r.Spec.APIKey == "" {
		key := types.NamespacedName{Namespace: r.Spec.APIKeySecret.Namespace, Name: r.Spec.APIKeySecret.Name}
		var apiKeySecret v1.Secret
		getErr := k8Client.Get(ctx, key, &apiKeySecret)
		if getErr != nil {
			log.Error(getErr, "Error getting secret")
			return getErr
		}
		apiKey = string(apiKeySecret.Data[r.Spec.APIKeySecret.KeyName])
	} else {
		apiKey = r.Spec.APIKey
	}

	alertsClient, errAlertClient := alertClientFunc(apiKey, r.Spec.Region)
	if errAlertClient != nil {
		log.Error(errAlertClient, "failed to get policy",
			"policyId", r.Spec.ExistingPolicyID,
			"API Key", interfaces.PartialAPIKey(apiKey),
			"region", r.Spec.Region,
		)
		return errAlertClient
	}

	alertPolicy, errAlertPolicy := alertsClient.GetPolicy(r.Spec.ExistingPolicyID)
	if errAlertPolicy != nil {
		if r.GetDeletionTimestamp() != nil {
			log.Info("Deleting resource", "errAlertPolicy", errAlertPolicy)
			if strings.Contains(errAlertPolicy.Error(), "no alert policy found for id") {
				log.Info("ExistingAlertPolicy not found but we are deleting the condition so this is ok")
				return nil
			}
		}
		log.Error(errAlertPolicy, "failed to get policy",
			"policyId", r.Spec.ExistingPolicyID,
			"API Key", interfaces.PartialAPIKey(apiKey),
			"region", r.Spec.Region,
		)
		return errAlertPolicy
	}

	if alertPolicy.ID != r.Spec.ExistingPolicyID {
		log.Info("Alert policy returned by the API failed to match provided policy ID")
		return errors.New("alert policy returned by API did not match")
	}

	return nil
}

func (r *NrqlAlertCondition) CheckForAPIKeyOrSecret() error {
	if r.Spec.APIKey != "" {
		return nil
	}

	if r.Spec.APIKeySecret != (NewRelicAPIKeySecret{}) {
		if r.Spec.APIKeySecret.Name != "" && r.Spec.APIKeySecret.Namespace != "" && r.Spec.APIKeySecret.KeyName != "" {
			return nil
		}
	}

	return errors.New("either api_key or api_key_secret must be set")
}

func (r *NrqlAlertCondition) CheckRequiredFields() error {
	missingFields := []string{}

	if r.Spec.Region == "" {
		missingFields = append(missingFields, "region")
	}

	if r.Spec.ExistingPolicyID == 0 {
		missingFields = append(missingFields, "existing_policy_id")
	}

	if len(missingFields) > 0 {
		return errors.New(strings.Join(missingFields, " and ") + " must be set")
	}

	return nil
}
