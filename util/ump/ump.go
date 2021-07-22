// Copyright 2018 The Chubao Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package ump

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type TpObject struct {
	StartTime time.Time
	EndTime   time.Time
	UmpType   interface{}
}

func NewTpObject() (o *TpObject) {
	o = new(TpObject)
	o.StartTime = time.Now()
	return
}

const (
	TpMethod        = "TP"
	HeartbeatMethod = "Heartbeat"
	FunctionError   = "FunctionError"
)

var (
	HostName      string
	LogTimeForMat = "20060102150405000"
	AlarmPool     = &sync.Pool{New: func() interface{} {
		return new(BusinessAlarm)
	}}
	TpObjectPool = &sync.Pool{New: func() interface{} {
		return new(TpObject)
	}}
	SystemAlivePool = &sync.Pool{New: func() interface{} {
		return new(SystemAlive)
	}}
	FunctionTpPool = &sync.Pool{New: func() interface{} {
		return new(FunctionTp)
	}}
	FunctionTpGroupByPool = &sync.Pool{New: func() interface{} {
		return new(FunctionTpGroupBy)
	}}
	enableUmp = true
)

func InitUmp(module string) (err error) {
	if err = initLogName(module); err != nil {
		return
	}

	backGroudWrite()
	return nil
}

func BeforeTP(key string) (o *TpObject) {
	if !enableUmp {
		return
	}

	o = TpObjectPool.Get().(*TpObject)
	o.StartTime = time.Now()
	tp := FunctionTpGroupByPool.Get().(*FunctionTpGroupBy)
	tp.HostName = HostName
	tp.currTime = o.StartTime
	tp.Key = key
	tp.ProcessState = "0"
	o.UmpType = tp

	return
}


func AfterTP(o *TpObject, err error) {
	if !enableUmp {
		return
	}
	tp := o.UmpType.(*FunctionTpGroupBy)
	tp.elapsedTime = (int64)(time.Since(o.StartTime) / 1e6)
	TpObjectPool.Put(o)
	tp.ProcessState = "0"
	if err != nil {
		tp.ProcessState = "1"
	}
	tp.count = 1
	internalKey:=tp.elapsedTime/50
	index:=internalKey%int64(FunctionTPMapCount)
	mkey := tp.Key + "_" + strconv.FormatInt( internalKey,10)
	v, ok := FuncationTPMap[index].Load(mkey)
	if !ok {
		FuncationTPMap[index].Store(mkey, tp)
	} else {
		atomic.AddInt64(&v.(*FunctionTpGroupBy).count, 1)
		atomic.AddInt64(&v.(*FunctionTpGroupBy).elapsedTime, tp.elapsedTime)
	}
}


var (
	FunctionTPMapCount=16
	FuncationTPMap []sync.Map
)

func init() {
	FuncationTPMap=make([]sync.Map,FunctionTPMapCount)
}

func AfterTPUs(o *TpObject, err error) {
	if !enableUmp {
		return
	}
	tp := o.UmpType.(*FunctionTpGroupBy)
	tp.elapsedTime = (int64)(time.Since(o.StartTime) / 1e3)
	TpObjectPool.Put(o)
	tp.ProcessState = "0"
	if err != nil {
		tp.ProcessState = "1"
	}
	tp.count = 1
	internalKey:=tp.elapsedTime/50
	index:=internalKey%int64(FunctionTPMapCount)
	mkey := tp.Key + "_" + strconv.FormatInt( internalKey,10)
	v, ok := FuncationTPMap[index].Load(mkey)
	if !ok {
		FuncationTPMap[index].Store(mkey, tp)
	} else {
		atomic.AddInt64(&v.(*FunctionTpGroupBy).count, 1)
		atomic.AddInt64(&v.(*FunctionTpGroupBy).elapsedTime, tp.elapsedTime)
	}
	return
}


func Alive(key string) {
	if !enableUmp {
		return
	}
	alive := SystemAlivePool.Get().(*SystemAlive)
	alive.HostName = HostName
	alive.Key = key
	alive.Time = time.Now().Format(LogTimeForMat)
	select {
	case SystemAliveLogWrite.logCh <- alive:
	default:
	}
	return
}

func Alarm(key, detail string) {
	if !enableUmp {
		return
	}
	alarm := AlarmPool.Get().(*BusinessAlarm)
	alarm.Time = time.Now().Format(LogTimeForMat)
	alarm.Key = key
	alarm.HostName = HostName
	alarm.BusinessType = "0"
	alarm.Value = "0"
	alarm.Detail = detail
	if len(alarm.Detail) > 512 {
		rs := []rune(detail)
		alarm.Detail = string(rs[0:510])
	}

	select {
	case BusinessAlarmLogWrite.logCh <- alarm:
		atomic.AddInt32(&BusinessAlarmLogWrite.inflight, 1)
	default:
	}
	return
}

func FlushAlarm() {
	if atomic.LoadInt32(&BusinessAlarmLogWrite.inflight) <= 0 {
		return
	}

	for {
		select {
		case <-BusinessAlarmLogWrite.empty:
			if atomic.LoadInt32(&BusinessAlarmLogWrite.inflight) <= 0 {
				return
			}
		}
	}
}
