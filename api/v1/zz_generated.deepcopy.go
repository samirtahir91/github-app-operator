//go:build !ignore_autogenerated

/*
Copyright 2024.

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

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GithubApp) DeepCopyInto(out *GithubApp) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GithubApp.
func (in *GithubApp) DeepCopy() *GithubApp {
	if in == nil {
		return nil
	}
	out := new(GithubApp)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GithubApp) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GithubAppList) DeepCopyInto(out *GithubAppList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]GithubApp, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GithubAppList.
func (in *GithubAppList) DeepCopy() *GithubAppList {
	if in == nil {
		return nil
	}
	out := new(GithubAppList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GithubAppList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GithubAppSpec) DeepCopyInto(out *GithubAppSpec) {
	*out = *in
	if in.RolloutDeployment != nil {
		in, out := &in.RolloutDeployment, &out.RolloutDeployment
		*out = new(RolloutDeploymentSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.VaultPrivateKey != nil {
		in, out := &in.VaultPrivateKey, &out.VaultPrivateKey
		*out = new(VaultPrivateKeySpec)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GithubAppSpec.
func (in *GithubAppSpec) DeepCopy() *GithubAppSpec {
	if in == nil {
		return nil
	}
	out := new(GithubAppSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GithubAppStatus) DeepCopyInto(out *GithubAppStatus) {
	*out = *in
	in.ExpiresAt.DeepCopyInto(&out.ExpiresAt)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GithubAppStatus.
func (in *GithubAppStatus) DeepCopy() *GithubAppStatus {
	if in == nil {
		return nil
	}
	out := new(GithubAppStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RolloutDeploymentSpec) DeepCopyInto(out *RolloutDeploymentSpec) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RolloutDeploymentSpec.
func (in *RolloutDeploymentSpec) DeepCopy() *RolloutDeploymentSpec {
	if in == nil {
		return nil
	}
	out := new(RolloutDeploymentSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VaultPrivateKeySpec) DeepCopyInto(out *VaultPrivateKeySpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VaultPrivateKeySpec.
func (in *VaultPrivateKeySpec) DeepCopy() *VaultPrivateKeySpec {
	if in == nil {
		return nil
	}
	out := new(VaultPrivateKeySpec)
	in.DeepCopyInto(out)
	return out
}
