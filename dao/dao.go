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
	"encoding/binary"
	"fmt"
	"path/filepath"

	gomysql "github.com/go-mysql-org/go-mysql/mysql"
	"github.com/juju/errors"
	"go.etcd.io/bbolt"

	"go-mysql-transfer/config"
	"go-mysql-transfer/dao/etcd"
	"go-mysql-transfer/dao/mysql"
	"go-mysql-transfer/dao/zookeeper"
	"go-mysql-transfer/domain/bo"
	"go-mysql-transfer/domain/constants"
	"go-mysql-transfer/domain/po"
	"go-mysql-transfer/util/fileutils"
	"go-mysql-transfer/util/log"
)

const (
	_storePath            = "db"
	_fileMode             = 0600
	_metadataFileName     = "metadata.db"
	_metadataTypeSource   = 1
	_metadataTypeEndpoint = 2
	_metadataTypePipeline = 3
)

var (
	_local          *bbolt.DB
	_positionBucket = []byte("position")
	_stateBucket    = []byte("state")
	_sourceBucket   = []byte("source")
	_endpointBucket = []byte("endpoint")
	_pipelineBucket = []byte("pipeline")
)

var (
	_compositeSourceDao   *CompositeSourceDao
	_compositeEndpointDao *CompositeEndpointDao
	_compositePipelineDao *CompositePipelineDao
	_positionDao          PositionDao
	_stateDao             StateDao
	_machineDao           MachineDao
	_remoteSourceDao      SourceDao
	_remoteEndpointDao    EndpointDao
	_remotePipelineDao    PipelineDao
)

type PositionDao interface {
	Save(pipelineId uint64, pos gomysql.Position) error
	Get(pipelineId uint64) gomysql.Position
}

type MachineDao interface {
	GetMachineIndex(machineUrl string) (uint16, error)
}

type StateDao interface {
	Save(pipelineId uint64, state *po.PipelineState) error
	IsExists(pipelineId uint64) (bool, error)
	Get(pipelineId uint64) (*po.PipelineState, error)
}

type SourceDao interface {
	Insert(id uint64, data []byte) error
	Delete(id uint64) error
	Update(id uint64, version int32, data []byte) error
	GetDataVersion(id uint64) (int32, error)
	Get(id uint64) (*po.SourceInfo, error)
	SelectAllDataVersion() ([]*po.MetadataVersion, error)
}

type EndpointDao interface {
	Insert(id uint64, data []byte) error
	Delete(id uint64) error
	Update(id uint64, version int32, data []byte) error
	GetDataVersion(id uint64) (int32, error)
	Get(id uint64) (*po.EndpointInfo, error)
	SelectAllDataVersion() ([]*po.MetadataVersion, error)
}

type PipelineDao interface {
	Insert(entity *po.PipelineInfo) error
	Delete(id uint64) error
	Update(entity *po.PipelineInfo, version int32) error
	GetDataVersion(id uint64) (int32, error)
	Get(id uint64) (*po.PipelineInfo, error)
	SelectAllDataVersion() ([]*po.MetadataVersion, error)
}

func Initialize(config *config.AppConfig) error {
	if err := initializeBoltDB(config); err != nil {
		return err
	}

	_compositeSourceDao = &CompositeSourceDao{}
	_compositeEndpointDao = &CompositeEndpointDao{}
	_compositePipelineDao = &CompositePipelineDao{}

	if config.IsCluster() { //集群
		switch config.GetClusterCoordinator() {
		case constants.ClusterCoordinatorEtcd:
			//	if err := initEtcd(config); err != nil {
			//		return err
			//	}
			//	_machineDao = &EtcdMachineDao{}
			//	_metadataDao = &EtcdMetadataDao{}
			//	_positionDao = &EtcdPositionDao{}
			//	_stateDao = &EtcdStateDao{}
		case constants.ClusterCoordinatorZookeeper:
			if err := zookeeper.Initialize(config); err != nil {
				return err
			}
			_machineDao = &zookeeper.MachineDaoImpl{}
			_positionDao = zookeeper.NewPositionDao()
			_stateDao = zookeeper.NewStateDao()
			_remoteSourceDao = &zookeeper.SourceDaoImpl{}
			_remoteEndpointDao = &zookeeper.EndpointDaoImpl{}
			_remotePipelineDao = &zookeeper.PipelineDaoImpl{}
		case constants.ClusterCoordinatorMySQL:
			if err := mysql.Initialize(config); err != nil {
				return err
			}
			_machineDao = &mysql.MachineDaoImpl{}
			_positionDao = mysql.NewPositionDao()
			_stateDao = mysql.NewStateDao()
			_metadataDao := &mysql.MetadataDao{}
			_remoteSourceDao = &mysql.SourceDaoImpl{
				MetadataType: _metadataTypeSource,
				MetadataDao:  _metadataDao,
			}
			_remoteEndpointDao = &mysql.EndpointDaoImpl{
				MetadataType: _metadataTypeEndpoint,
				MetadataDao:  _metadataDao,
			}
			_remotePipelineDao = &mysql.PipelineDaoImpl{
				MetadataType: _metadataTypePipeline,
				MetadataDao:  _metadataDao,
			}
		default:
			return errors.New("请配置分布式协调器")
		}
	} else {
		_positionDao = &LocalPositionDao{}
		_stateDao = &LocalStateDao{}
	}

	return nil
}

// RefreshMetadata 刷新本地元数据
func RefreshMetadata() {
	sources, err := _remoteSourceDao.SelectAllDataVersion()
	if err != nil {
		panic(fmt.Sprintf("刷新[SourceInfo]数据失败[%s]", err.Error()))
	}
	if err = _compositeSourceDao.refreshAll(sources); err != nil {
		panic(fmt.Sprintf("刷新[SourceInfo]元数据失败[%s]", err.Error()))
	}

	var endpoints []*po.MetadataVersion
	endpoints, err = _remoteEndpointDao.SelectAllDataVersion()
	if err != nil {
		panic(fmt.Sprintf("刷新[EndpointInfo]数据失败[%s]", err.Error()))
	}
	if err = _compositeEndpointDao.refreshAll(endpoints); err != nil {
		panic(fmt.Sprintf("刷新[EndpointInfo]元数据失败[%s]", err.Error()))
	}

	var pipelines []*po.MetadataVersion
	pipelines, err = _remotePipelineDao.SelectAllDataVersion()
	if err != nil {
		panic(fmt.Sprintf("刷新[PipelineInfo]数据失败[%s]", err.Error()))
	}
	if err = _compositePipelineDao.refreshAll(pipelines); err != nil {
		panic(fmt.Sprintf("刷新[PipelineInfo]元数据失败[%s]", err.Error()))
	}
}

func OnSyncEvent(event *bo.SyncEvent) {
	switch event.Type {
	case constants.SyncEventTypeSource:
		err := _compositeSourceDao.refreshOne(event.Id, event.Version)
		if err != nil {
			log.Errorf("刷新[SourceInfo]数据失败[%s]", err.Error())
		}
	case constants.SyncEventTypeEndpoint:
		err := _compositeEndpointDao.refreshOne(event.Id, event.Version)
		if err != nil {
			log.Errorf("刷新[EndpointInfo]数据失败[%s]", err.Error())
		}
	case constants.SyncEventTypePipeline:
		err := _compositePipelineDao.refreshOne(event.Id, event.Version)
		if err != nil {
			log.Errorf("刷新[PipelineInfo]数据失败[%s]", err.Error())
		}
	}
}

func Close() {
	if nil != _local {
		_local.Close()
	}
	zookeeper.Close()
	etcd.Close()
}

func GetSourceInfoDao() *CompositeSourceDao {
	return _compositeSourceDao
}

func GetEndpointInfoDao() *CompositeEndpointDao {
	return _compositeEndpointDao
}

func GetPipelineInfoDao() *CompositePipelineDao {
	return _compositePipelineDao
}

func GetPositionDao() PositionDao {
	return _positionDao
}

func GetStateDao() StateDao {
	return _stateDao
}

func GetMachineDao() MachineDao {
	return _machineDao
}

func initializeBoltDB(config *config.AppConfig) error {
	storePath := filepath.Join(config.GetDataDir(), _storePath)
	if err := fileutils.MkdirIfNecessary(storePath); err != nil {
		return errors.New(fmt.Sprintf("创建元数据存储目录失败：%s", err.Error()))
	}

	var err error

	metadataFilePath := filepath.Join(storePath, _metadataFileName)
	_local, err = bbolt.Open(metadataFilePath, _fileMode, bbolt.DefaultOptions)
	if err != nil {
		return errors.New(fmt.Sprintf("打开BoltDB失败：%s", err.Error()))
	}

	err = _local.Update(func(tx *bbolt.Tx) error {
		if _, err = tx.CreateBucketIfNotExists(_positionBucket); err != nil {
			return err
		}
		if _, err = tx.CreateBucketIfNotExists(_stateBucket); err != nil {
			return err
		}
		if _, err = tx.CreateBucketIfNotExists(_sourceBucket); err != nil {
			return err
		}
		if _, err = tx.CreateBucketIfNotExists(_endpointBucket); err != nil {
			return err
		}
		if _, err = tx.CreateBucketIfNotExists(_pipelineBucket); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return errors.New(fmt.Sprintf("创建BoltDB存储桶失败：%s", err.Error()))
	}

	return nil
}

func marshalId(id uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, id)
	return buf
}