/*
 * Copyright 2021-2022 the original author(https://github.com/wj596)
 *
 * <p>
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 * </p>
 */

package httpapi

import (
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/yuin/gopher-lua"

	"go-mysql-transfer/domain/bo"
	"go-mysql-transfer/domain/po"
)

type BatchEndpoint struct {
	endpoint *Endpoint
}

func NewBatchEndpoint(info *po.EndpointInfo) *BatchEndpoint {
	return &BatchEndpoint{
		endpoint: &Endpoint{
			info: info,
		},
	}
}

func (s *BatchEndpoint) Batch(requests []*bo.RowEventRequest, ctx *bo.RuleContext, lvm *lua.LState) (int64, error) {
	var messages []*primitive.Message
	if ctx.IsLuaEnable() {
		for _, request := range requests {
			err := s.endpoint.parseByLua(request, ctx, lvm)
			if err != nil {
				return 0, err
			}
		}
	} else {
		for _, request := range requests {
			err := s.endpoint.parseByRegular(request, ctx)
			if err != nil {
				return 0, err
			}
		}
	}

	if len(messages) == 0 {
		return 0, nil
	}

	return int64(len(messages)), nil
}

func (s *BatchEndpoint) Connect() error {
	return s.endpoint.Connect()
}

func (s *BatchEndpoint) Ping() error {
	return s.endpoint.Ping()
}

func (s *BatchEndpoint) Close() {
	s.endpoint.Close()
}