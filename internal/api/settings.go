package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/logger"
	"github.com/makinuki/makidoku/internal/settings"
)

type settingResponse struct {
	Key         string `json:"key"`
	Value       any    `json:"value"`
	Default     any    `json:"default"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

func typedSetting(entry settings.Entry) (settingResponse, error) {
	var value, defaultValue any
	if err := json.Unmarshal([]byte(entry.Value), &value); err != nil {
		return settingResponse{}, err
	}
	if err := json.Unmarshal([]byte(entry.Default), &defaultValue); err != nil {
		return settingResponse{}, err
	}
	return settingResponse{Key: entry.Key, Value: value, Default: defaultValue, Type: entry.Type, Description: entry.Description}, nil
}

var errSettingMissing = errors.New("setting was not found after update")

func marshalSettingValue(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Server) mountSettings(r chi.Router) {
	r.Get("/settings", s.listSettings)
	r.Put("/settings/{key}", s.putSetting)
	r.Post("/settings/reset", s.resetSettings)
}

func (s *Server) listSettings(w http.ResponseWriter, r *http.Request) {
	entries, err := s.settings.List()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	result := make([]settingResponse, 0, len(entries))
	for _, entry := range entries {
		item, err := typedSetting(entry)
		if err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
		result = append(result, item)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) putSetting(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	var body struct {
		Value any `json:"value"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Value == nil {
		writeBadRequest(w, "value is required")
		return
	}
	raw, err := marshalSettingValue(body.Value)
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	if err := s.settings.Set(key, raw); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	if key == "advanced.log_level" {
		_ = logger.SetLevelFromRaw(raw)
	}
	entries, err := s.settings.List()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	for _, entry := range entries {
		if entry.Key == key {
			item, err := typedSetting(entry)
			if err != nil {
				writeLocalError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeLocalError(w, http.StatusInternalServerError, errSettingMissing)
}

func (s *Server) resetSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keys []string `json:"keys"`
		All  bool     `json:"all"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if !body.All && len(body.Keys) == 0 {
		writeBadRequest(w, "keys or all is required")
		return
	}
	if err := s.settings.Reset(body.Keys, body.All); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	if body.All {
		if raw, err := s.settings.Get("advanced.log_level"); err == nil {
			_ = logger.SetLevelFromRaw(raw)
		}
	} else {
		for _, key := range body.Keys {
			if key == "advanced.log_level" {
				if raw, err := s.settings.Get(key); err == nil {
					_ = logger.SetLevelFromRaw(raw)
				}
				break
			}
		}
	}
	s.listSettings(w, r)
}
