// Copyright 2023 - MinIO, Inc. All rights reserved.
// Use of this source code is governed by the AGPLv3
// license that can be found in the LICENSE file.

package api

import (
	"encoding/json"
	"net/http"
	"path"
	"time"

	"aead.dev/mem"
	"github.com/minio/kes-go"
	"github.com/minio/kes/internal/audit"
	"github.com/minio/kes/internal/auth"
)

func assignPolicy(config *RouterConfig) API {
	const (
		Method  = http.MethodPost
		APIPath = "/v1/policy/assign/"
		MaxBody = int64(1 * mem.KiB)
		Timeout = 15 * time.Second
		Verify  = true
	)
	type Request struct {
		Identity kes.Identity `json:"identity"`
	}
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		name, err := nameFromRequest(r, APIPath)
		if err != nil {
			return err
		}

		if err = Sync(config.Vault.RLocker(), func() error {
			enclave, err := enclaveFromRequest(config.Vault, r)
			if err != nil {
				return err
			}
			return Sync(enclave.Locker(), func() error {
				if err = enclave.VerifyRequest(r); err != nil {
					return err
				}

				var req Request
				if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
					return err
				}
				if err = verifyName(req.Identity.String()); err != nil {
					return err
				}
				if req.Identity.IsUnknown() {
					return kes.NewError(http.StatusBadRequest, "identity is unknown")
				}
				if self := auth.Identify(r); self == req.Identity {
					return kes.NewError(http.StatusForbidden, "identity cannot assign policy to itself")
				}
				admin, err := config.Vault.Admin(r.Context())
				if err != nil {
					return err
				}
				if admin == req.Identity {
					return kes.NewError(http.StatusBadRequest, "cannot assign policy to system admin")
				}
				return enclave.AssignPolicy(r.Context(), name, req.Identity)
			})
		}); err != nil {
			return err
		}

		w.WriteHeader(http.StatusOK)
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}

func describePolicy(config *RouterConfig) API {
	const (
		Method      = http.MethodGet
		APIPath     = "/v1/policy/describe/"
		MaxBody     = 0
		Timeout     = 15 * time.Second
		Verify      = true
		ContentType = "application/json"
	)
	type Response struct {
		CreatedAt time.Time    `json:"created_at,omitempty"`
		CreatedBy kes.Identity `json:"created_by,omitempty"`
	}
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		name, err := nameFromRequest(r, APIPath)
		if err != nil {
			return err
		}

		policy, err := VSync(config.Vault.RLocker(), func() (auth.Policy, error) {
			enclave, err := enclaveFromRequest(config.Vault, r)
			if err != nil {
				return auth.Policy{}, err
			}
			return VSync(enclave.RLocker(), func() (auth.Policy, error) {
				if err = enclave.VerifyRequest(r); err != nil {
					return auth.Policy{}, err
				}
				return enclave.GetPolicy(r.Context(), name)
			})
		})
		if err != nil {
			return err
		}

		w.Header().Set("Content-Type", ContentType)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(Response{
			CreatedAt: policy.CreatedAt,
			CreatedBy: policy.CreatedBy,
		})
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}

func edgeDescribePolicy(config *EdgeRouterConfig) API {
	var (
		Method      = http.MethodGet
		APIPath     = "/v1/policy/describe/"
		MaxBody     int64
		Timeout     = 15 * time.Second
		Verify      = true
		ContentType = "application/json"
	)
	if c, ok := config.APIConfig[APIPath]; ok {
		if c.Timeout > 0 {
			Timeout = c.Timeout
		}
	}
	type Response struct {
		CreatedAt time.Time    `json:"created_at,omitempty"`
		CreatedBy kes.Identity `json:"created_by,omitempty"`
	}
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		name, err := nameFromRequest(r, APIPath)
		if err != nil {
			return err
		}
		if err := auth.VerifyRequest(r, config.Policies, config.Identities); err != nil {
			return err
		}

		policy, err := config.Policies.Get(r.Context(), name)
		if err != nil {
			return err
		}

		w.Header().Set("Content-Type", ContentType)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(Response{
			CreatedAt: policy.CreatedAt,
			CreatedBy: policy.CreatedBy,
		})
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}

func readPolicy(config *RouterConfig) API {
	const (
		Method      = http.MethodGet
		APIPath     = "/v1/policy/read/"
		MaxBody     = 0
		Timeout     = 15 * time.Second
		Verify      = true
		ContentType = "application/json"
	)
	type Response struct {
		Allow     []string     `json:"allow,omitempty"`
		Deny      []string     `json:"deny,omitempty"`
		CreatedAt time.Time    `json:"created_at,omitempty"`
		CreatedBy kes.Identity `json:"created_by,omitempty"`
	}
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		name, err := nameFromRequest(r, APIPath)
		if err != nil {
			return err
		}

		policy, err := VSync(config.Vault.RLocker(), func() (auth.Policy, error) {
			enclave, err := enclaveFromRequest(config.Vault, r)
			if err != nil {
				return auth.Policy{}, err
			}
			return VSync(enclave.RLocker(), func() (auth.Policy, error) {
				if err = enclave.VerifyRequest(r); err != nil {
					return auth.Policy{}, err
				}
				return enclave.GetPolicy(r.Context(), name)
			})
		})
		if err != nil {
			return err
		}

		w.Header().Set("Content-Type", ContentType)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(Response{
			Allow:     policy.Allow,
			Deny:      policy.Deny,
			CreatedAt: policy.CreatedAt,
			CreatedBy: policy.CreatedBy,
		})
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}

func edgeReadPolicy(config *EdgeRouterConfig) API {
	var (
		Method      = http.MethodGet
		APIPath     = "/v1/policy/read/"
		MaxBody     int64
		Timeout     = 15 * time.Second
		Verify      = true
		ContentType = "application/json"
	)
	if c, ok := config.APIConfig[APIPath]; ok {
		if c.Timeout > 0 {
			Timeout = c.Timeout
		}
	}
	type Response struct {
		Allow     []string     `json:"allow,omitempty"`
		Deny      []string     `json:"deny,omitempty"`
		CreatedAt time.Time    `json:"created_at,omitempty"`
		CreatedBy kes.Identity `json:"created_by,omitempty"`
	}
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		name, err := nameFromRequest(r, APIPath)
		if err != nil {
			return err
		}
		if err := auth.VerifyRequest(r, config.Policies, config.Identities); err != nil {
			return err
		}

		policy, err := config.Policies.Get(r.Context(), name)
		if err != nil {
			return err
		}

		w.Header().Set("Content-Type", ContentType)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(Response{
			Allow:     policy.Allow,
			Deny:      policy.Deny,
			CreatedAt: policy.CreatedAt,
			CreatedBy: policy.CreatedBy,
		})
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}

func writePolicy(config *RouterConfig) API {
	const (
		Method  = http.MethodPost
		APIPath = "/v1/policy/write/"
		MaxBody = int64(1 * mem.MiB)
		Timeout = 15 * time.Second
		Verify  = true
	)
	type Request struct {
		Allow []string `json:"allow,omitempty"`
		Deny  []string `json:"deny,omitempty"`
	}
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		name, err := nameFromRequest(r, APIPath)
		if err != nil {
			return err
		}

		if err = Sync(config.Vault.RLocker(), func() error {
			enclave, err := enclaveFromRequest(config.Vault, r)
			if err != nil {
				return err
			}
			return Sync(enclave.Locker(), func() error {
				if err = enclave.VerifyRequest(r); err != nil {
					return err
				}

				var req Request
				if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
					return err
				}
				return enclave.SetPolicy(r.Context(), name, auth.Policy{
					Allow:     req.Allow,
					Deny:      req.Deny,
					CreatedAt: time.Now().UTC(),
					CreatedBy: auth.Identify(r),
				})
			})
		}); err != nil {
			return err
		}

		w.WriteHeader(http.StatusOK)
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}

func deletePolicy(config *RouterConfig) API {
	const (
		Method  = http.MethodDelete
		APIPath = "/v1/policy/delete/"
		MaxBody = 0
		Timeout = 15 * time.Second
		Verify  = true
	)
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		name, err := nameFromRequest(r, APIPath)
		if err != nil {
			return err
		}

		if err = Sync(config.Vault.RLocker(), func() error {
			enclave, err := enclaveFromRequest(config.Vault, r)
			if err != nil {
				return err
			}
			return Sync(enclave.Locker(), func() error {
				if err = enclave.VerifyRequest(r); err != nil {
					return err
				}
				return enclave.DeletePolicy(r.Context(), name)
			})
		}); err != nil {
			return err
		}

		w.WriteHeader(http.StatusOK)
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}

func listPolicy(config *RouterConfig) API {
	const (
		Method      = http.MethodGet
		APIPath     = "/v1/policy/list/"
		MaxBody     = 0
		Timeout     = 15 * time.Second
		Verify      = true
		ContentType = "application/x-ndjson"
	)
	type Response struct {
		Name      string       `json:"name"`
		CreatedAt time.Time    `json:"created_at,omitempty"`
		CreatedBy kes.Identity `json:"created_by,omitempty"`

		Err string `json:"error,omitempty"`
	}
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		pattern, err := patternFromRequest(r, APIPath)
		if err != nil {
			return err
		}

		hasWritten, err := VSync(config.Vault.RLocker(), func() (bool, error) {
			enclave, err := enclaveFromRequest(config.Vault, r)
			if err != nil {
				return false, err
			}
			return VSync(enclave.RLocker(), func() (bool, error) {
				if err = enclave.VerifyRequest(r); err != nil {
					return false, err
				}
				iterator, err := enclave.ListPolicies(r.Context())
				if err != nil {
					return false, err
				}
				defer iterator.Close()

				var hasWritten bool
				encoder := json.NewEncoder(w)
				for iterator.Next() {
					if ok, _ := path.Match(pattern, iterator.Name()); !ok {
						continue
					}
					if !hasWritten {
						hasWritten = true
						w.Header().Set("Content-Type", ContentType)
						w.WriteHeader(http.StatusOK)
					}

					policy, err := enclave.GetPolicy(r.Context(), iterator.Name())
					if err != nil {
						return hasWritten, err
					}
					err = encoder.Encode(Response{
						Name:      iterator.Name(),
						CreatedAt: policy.CreatedAt,
						CreatedBy: policy.CreatedBy,
					})
					if err != nil {
						return hasWritten, err
					}
				}
				return hasWritten, iterator.Close()
			})
		})
		if err != nil {
			if hasWritten {
				json.NewEncoder(w).Encode(Response{Err: err.Error()})
				return nil
			}
			return err
		}
		if !hasWritten {
			w.WriteHeader(http.StatusOK)
		}
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}

func edgeListPolicy(config *EdgeRouterConfig) API {
	var (
		Method      = http.MethodGet
		APIPath     = "/v1/policy/list/"
		MaxBody     int64
		Timeout     = 15 * time.Second
		Verify      = true
		ContentType = "application/x-ndjson"
	)
	if c, ok := config.APIConfig[APIPath]; ok {
		if c.Timeout > 0 {
			Timeout = c.Timeout
		}
	}
	type Response struct {
		Name      string       `json:"name"`
		CreatedAt time.Time    `json:"created_at,omitempty"`
		CreatedBy kes.Identity `json:"created_by,omitempty"`

		Err string `json:"error,omitempty"`
	}
	var handler HandlerFunc = func(w http.ResponseWriter, r *http.Request) error {
		pattern, err := patternFromRequest(r, APIPath)
		if err != nil {
			return err
		}
		if err := auth.VerifyRequest(r, config.Policies, config.Identities); err != nil {
			return err
		}

		iterator, err := config.Policies.List(r.Context())
		if err != nil {
			return err
		}
		defer iterator.Close()

		var hasWritten bool
		encoder := json.NewEncoder(w)
		w.Header().Set("Content-Type", ContentType)
		for iterator.Next() {
			if ok, _ := path.Match(pattern, iterator.Name()); !ok {
				continue
			}
			if !hasWritten {
				w.Header().Set("Content-Type", ContentType)
			}
			hasWritten = true

			policy, err := config.Policies.Get(r.Context(), iterator.Name())
			if err != nil {
				encoder.Encode(Response{Err: err.Error()})
				return nil
			}
			if err = encoder.Encode(Response{
				Name:      iterator.Name(),
				CreatedAt: policy.CreatedAt,
				CreatedBy: policy.CreatedBy,
			}); err != nil {
				return nil
			}
		}
		if err = iterator.Close(); err != nil {
			if hasWritten {
				encoder.Encode(Response{Err: err.Error()})
				return nil
			}
			return err
		}
		if !hasWritten {
			w.Header().Set("Content-Type", ContentType)
			w.WriteHeader(http.StatusOK)
		}
		return nil
	}
	return API{
		Method:  Method,
		Path:    APIPath,
		MaxBody: MaxBody,
		Timeout: Timeout,
		Verify:  Verify,
		Handler: config.Metrics.Count(config.Metrics.Latency(audit.Log(config.AuditLog, handler))),
	}
}
