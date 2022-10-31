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

package dao

import (
	"strings"

	"github.com/juju/errors"
	"go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"

	"go-mysql-transfer/domain/constants"
	"go-mysql-transfer/domain/po"
	"go-mysql-transfer/domain/vo"
	"go-mysql-transfer/util/byteutils"
	"go-mysql-transfer/util/log"
)

type CompositePipelineDao struct {
}

func (s *CompositePipelineDao) Save(entity *po.PipelineInfo) error {
	return _local.Update(func(tx *bbolt.Tx) error {
		data, err := proto.Marshal(entity)
		if err != nil {
			return err
		}
		return tx.Bucket(_pipelineBucket).Put(marshalId(entity.Id), data)
	})
}

func (s *CompositePipelineDao) CascadeInsert(entity *po.PipelineInfo) error {
	err := _local.Update(func(tx *bbolt.Tx) error {
		data, err := proto.Marshal(entity)
		if err != nil {
			return err
		}

		err = tx.Bucket(_pipelineBucket).Put(marshalId(entity.Id), data)
		if err != nil {
			return err
		}

		err = _remotePipelineDao.Insert(entity)
		return err
	})

	if nil == err {
		log.Infof("CascadeInsert PipelineInfo[%s]", entity.Name)
	}
	return err
}

func (s *CompositePipelineDao) CascadeUpdate(entity *po.PipelineInfo) (int32, error) {
	version, err := _remotePipelineDao.GetDataVersion(entity.Id)
	if err != nil {
		return 0, err
	}
	entity.DataVersion = version + 1

	err = _local.Update(func(tx *bbolt.Tx) error {
		data, err := proto.Marshal(entity)
		if err != nil {
			return err
		}

		err = tx.Bucket(_pipelineBucket).Put(marshalId(entity.Id), data)
		if err != nil {
			return err
		}

		err = _remotePipelineDao.Update(entity, version)
		return err
	})

	if err != nil {
		return version, err
	}

	log.Infof("CascadeUpdate PipelineInfo[%s], Version[%d]", entity.Name, version)
	return entity.DataVersion, nil
}

func (s *CompositePipelineDao) Delete(id uint64) error {
	return _local.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(_pipelineBucket).Delete(marshalId(id))
	})
}

func (s *CompositePipelineDao) CascadeDelete(id uint64) error {
	err := _local.Update(func(tx *bbolt.Tx) error {
		err := tx.Bucket(_pipelineBucket).Delete(marshalId(id))
		if err != nil {
			return err
		}
		return _remotePipelineDao.Delete(id)
	})

	if nil == err {
		log.Infof("CascadeDelete PipelineInfo[%d]", id)
	}
	return err
}

func (s *CompositePipelineDao) Get(id uint64) (*po.PipelineInfo, error) {
	var entity po.PipelineInfo
	err := _local.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(_pipelineBucket).Get(marshalId(id))
		if data == nil {
			return errors.NotFoundf("PipelineInfo")
		}
		return proto.Unmarshal(data, &entity)
	})

	if err != nil {
		return nil, err
	}

	return &entity, err
}

func (s *CompositePipelineDao) GetByParam(params *vo.PipelineInfoParams) (*po.PipelineInfo, error) {
	var entity po.PipelineInfo
	var found bool
	err := _local.View(func(tx *bbolt.Tx) error {
		cursor := tx.Bucket(_pipelineBucket).Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			if err := proto.Unmarshal(v, &entity); err == nil {
				if params.Name != "" && entity.Name != params.Name {
					continue
				}
				if params.SourceId != 0 && entity.SourceId != params.SourceId {
					continue
				}
				if params.EndpointId != 0 && entity.EndpointId != params.EndpointId {
					continue
				}
				found = true
				break
			}
		}
		return nil
	})

	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	if !found {
		return nil, errors.NotFoundf("PipelineInfo")
	}

	return &entity, err
}

func (s *CompositePipelineDao) SelectList(params *vo.PipelineInfoParams) ([]*po.PipelineInfo, error) {
	list := make([]*po.PipelineInfo, 0)
	err := _local.View(func(tx *bbolt.Tx) error {
		cursor := tx.Bucket(_pipelineBucket).Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var entity po.PipelineInfo
			if err := proto.Unmarshal(v, &entity); err == nil {
				if params.Name != "" && !strings.Contains(entity.Name, params.Name) {
					continue
				}
				if params.SourceId != 0 && entity.SourceId != params.SourceId {
					continue
				}
				if params.EndpointId != 0 && entity.EndpointId != params.EndpointId {
					continue
				}
				if params.EndpointType != 0 && entity.EndpointType != params.EndpointType {
					continue
				}
				if params.Enable && entity.Status == constants.PipelineInfoStatusDisable {
					continue
				}
				list = append(list, &entity)
			}
		}
		return nil
	})

	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	return list, err
}

func (s *CompositePipelineDao) refreshAll(standards []*po.MetadataVersion) error {
	locals := make([]uint64, 0)
	_local.View(func(tx *bbolt.Tx) error {
		bt := tx.Bucket(_pipelineBucket)
		cursor := bt.Cursor()
		for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
			locals = append(locals, byteutils.BytesToUint64(k))
		}
		return nil
	})

	deletions := make([]uint64, 0)
	for _, id := range locals {
		isRemove := true
		for _, standard := range standards {
			if id == standard.Id {
				isRemove = false
				break
			}
		}
		if isRemove {
			deletions = append(deletions, id)
		}
	}
	log.Infof("刷新[PipelineInfo]数据, 需删除本地数据[%d]条", len(deletions))

	updates := make([]*po.PipelineInfo, 0)
	for _, standard := range standards {
		version := int32(-1)
		entity, _ := s.Get(standard.Id)
		if nil != entity && entity.DataVersion >= standard.Version {
			log.Infof("忽略刷新[PipelineInfo]数据[1]条，名称[%s]， 本地数据版本[%d]，远程数据版本[%d]", entity.Name, entity.DataVersion, standard.Version)
			continue
		}

		if nil != entity {
			version = entity.DataVersion
		}

		standardEntity, err := _remotePipelineDao.Get(standard.Id)
		if err != nil {
			return err
		}
		log.Infof("刷新[PipelineInfo]数据[1]条，名称[%s]， 本地数据版本[%d]，远程数据版本[%d]", standardEntity.Name, version, standard.Version)
		updates = append(updates, standardEntity)
	}

	err := _local.Update(func(tx *bbolt.Tx) error {
		bt := tx.Bucket(_pipelineBucket)
		for _, deletion := range deletions {
			if err := bt.Delete(marshalId(deletion)); err != nil {
				return err
			}
		}
		for _, update := range updates {
			data, err := proto.Marshal(update)
			if err != nil {
				return err
			}
			err = tx.Bucket(_pipelineBucket).Put(marshalId(update.Id), data)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	log.Infof("共刷新[PipelineInfo]数据[%d]条", len(deletions)+len(updates))
	return nil
}

func (s *CompositePipelineDao) refreshOne(id uint64, standardVersion int32) error {
	standardEntity, err := _remotePipelineDao.Get(id)
	if err != nil {
		return err
	}

	version := int32(-1)
	var entity *po.PipelineInfo
	entity, err = s.Get(id)
	if err != nil {
		return err
	}
	if nil != entity {
		version = entity.DataVersion
	}

	//远程为空，本地为空，删除本地
	if nil == standardEntity && nil != entity {
		log.Infof("删除[PipelineInfo]数据[1]条，名称[%s], 数据版本[%d]", entity.Name, entity.DataVersion)
		return s.Delete(id)
	}

	if nil != standardEntity && version < standardVersion {
		log.Infof("刷新[PipelineInfo]数据[1]条，名称[%s]， 本地数据版本[%d]，远程数据版本[%d]", id, standardEntity.Name, version, standardEntity.DataVersion)
		return s.Save(standardEntity)
	}

	return nil
}