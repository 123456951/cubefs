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

package data

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"time"

	"github.com/chubaofs/chubaofs/proto"
	"github.com/chubaofs/chubaofs/util"
)

// Packet defines a wrapper of the packet in proto.
type Packet struct {
	proto.Packet
	inode    uint64
	errCount int
}

// String returns the string format of the packet.
func (p *Packet) String() string {
	return fmt.Sprintf("ReqID(%v)Op(%v)Inode(%v)FileOffset(%v)Size(%v)PartitionID(%v)ExtentID(%v)ExtentOffset(%v)CRC(%v)ResultCode(%v)",
		p.ReqID, p.GetOpMsg(), p.inode, p.KernelOffset, p.Size, p.PartitionID, p.ExtentID, p.ExtentOffset, p.CRC, p.GetResultMsg())
}

// NewWritePacket returns a new write packet.
func NewWritePacket(ctx context.Context, inode uint64, fileOffset, storeMode int, blksize int) *Packet {
	p := new(Packet)
	p.ReqID = proto.GenerateRequestID()
	p.Magic = proto.ProtoMagic
	p.Opcode = proto.OpWrite
	p.ExtentType = uint8(storeMode)
	p.inode = inode
	p.KernelOffset = uint64(fileOffset)
	var err error
	if p.Data, err = proto.Buffers.Get(blksize); err != nil {
		p.Data = make([]byte, blksize)
	}
	p.SetCtx(ctx)
	return p
}

// NewWritePacket returns a new write packet.
func NewROWPacket(ctx context.Context, dp *DataPartition, inode uint64, extID, fileOffset, extentOffset, size int) *Packet {
	p := new(Packet)
	p.ReqID = proto.GenerateRequestID()
	p.Magic = proto.ProtoMagic
	p.Opcode = proto.OpWrite
	p.ExtentType = proto.NormalExtentType
	p.PartitionID = dp.PartitionID
	p.ExtentID = uint64(extID)
	p.Arg = ([]byte)(dp.GetAllAddrs())
	p.ArgLen = uint32(len(p.Arg))
	p.RemainingFollowers = uint8(len(dp.Hosts) - 1)
	p.KernelOffset = uint64(fileOffset)
	p.ExtentOffset = int64(extentOffset)
	p.Size = uint32(size)
	p.inode = inode

	p.SetCtx(ctx)
	return p
}

// NewOverwritePacket returns a new overwrite packet.
func NewOverwritePacket(ctx context.Context, dp *DataPartition, extentID uint64, extentOffset int, inode uint64, fileOffset int) *Packet {
	p := new(Packet)
	p.PartitionID = dp.PartitionID
	p.Magic = proto.ProtoMagic
	p.ExtentType = proto.NormalExtentType
	p.ExtentID = extentID
	p.ExtentOffset = int64(extentOffset)
	p.ReqID = proto.GenerateRequestID()
	p.Arg = nil
	p.ArgLen = 0
	p.RemainingFollowers = 0
	p.Opcode = proto.OpRandomWrite
	p.inode = inode
	p.KernelOffset = uint64(fileOffset)
	p.SetCtx(ctx)
	return p
}

// NewReadPacket returns a new read packet.
func NewReadPacket(ctx context.Context, key *proto.ExtentKey, extentOffset, size int, inode uint64, fileOffset int, followerRead bool) *Packet {
	p := new(Packet)
	p.ExtentID = key.ExtentId
	p.PartitionID = key.PartitionId
	p.Magic = proto.ProtoMagic
	p.ExtentOffset = int64(extentOffset)
	p.Size = uint32(size)
	if followerRead {
		p.Opcode = proto.OpStreamFollowerRead
	} else {
		p.Opcode = proto.OpStreamRead
	}
	p.ExtentType = proto.NormalExtentType
	p.ReqID = proto.GenerateRequestID()
	p.RemainingFollowers = 0
	p.inode = inode
	p.KernelOffset = uint64(fileOffset)
	p.SetCtx(ctx)
	return p
}

// NewCreateExtentPacket returns a new packet to create extent.
func NewCreateExtentPacket(ctx context.Context, dp *DataPartition, inode uint64) *Packet {
	p := new(Packet)
	p.PartitionID = dp.PartitionID
	p.Magic = proto.ProtoMagic
	p.ExtentType = proto.NormalExtentType
	p.Arg = ([]byte)(dp.GetAllAddrs())
	p.ArgLen = uint32(len(p.Arg))
	p.RemainingFollowers = uint8(len(dp.Hosts) - 1)
	p.ReqID = proto.GenerateRequestID()
	p.Opcode = proto.OpCreateExtent
	p.Data = make([]byte, 8)
	binary.BigEndian.PutUint64(p.Data, inode)
	p.Size = uint32(len(p.Data))
	p.SetCtx(ctx)
	return p
}

// NewPacketToGetAppliedID returns a new packet to get the applied ID.
func NewPacketToGetDpAppliedID(ctx context.Context, partitionID uint64) (p *Packet) {
	p = new(Packet)
	p.Opcode = proto.OpGetAppliedId
	p.PartitionID = partitionID
	p.Magic = proto.ProtoMagic
	p.ReqID = proto.GenerateRequestID()
	p.Arg = nil
	p.ArgLen = 0
	p.RemainingFollowers = 0
	p.SetCtx(ctx)
	return
}

// NewReply returns a new reply packet. TODO rename to NewReplyPacket?
func NewReply(ctx context.Context, reqID int64, partitionID uint64, extentID uint64) *Packet {
	p := new(Packet)
	p.ReqID = reqID
	p.PartitionID = partitionID
	p.ExtentID = extentID
	p.Magic = proto.ProtoMagic
	p.ExtentType = proto.NormalExtentType
	p.SetCtx(ctx)
	return p
}

func (p *Packet) isValidWriteReply(q *Packet) bool {
	if p.ReqID == q.ReqID && p.PartitionID == q.PartitionID {
		return true
	}
	return false
}

func (p *Packet) isValidReadReply(q *Packet) bool {
	if p.ReqID == q.ReqID && p.PartitionID == q.PartitionID && p.ExtentID == q.ExtentID {
		return true
	}
	return false
}

func (p *Packet) writeToConn(conn net.Conn) error {
	p.CRC = crc32.ChecksumIEEE(p.Data[:p.Size])
	return p.WriteToConn(conn)
}

func (p *Packet) readFromConn(c net.Conn, deadlineTime time.Duration) (err error) {
	if deadlineTime != proto.NoReadDeadlineTime {
		c.SetReadDeadline(time.Now().Add(deadlineTime * time.Second))
	}
	header, _ := proto.Buffers.Get(util.PacketHeaderSize)
	defer proto.Buffers.Put(header)
	if _, err = io.ReadFull(c, header); err != nil {
		return
	}
	if err = p.UnmarshalHeader(header); err != nil {
		return
	}

	if p.ArgLen > 0 {
		if err = readToBuffer(c, &p.Arg, int(p.ArgLen)); err != nil {
			return
		}
	}

	if p.Size < 0 {
		return
	}

	size := int(p.Size)
	if size > len(p.Data) {
		size = len(p.Data)
	}

	_, err = io.ReadFull(c, p.Data[:size])
	return
}

func readToBuffer(c net.Conn, buf *[]byte, readSize int) (err error) {
	if *buf == nil || readSize != util.BlockSize {
		*buf = make([]byte, readSize)
	}
	_, err = io.ReadFull(c, (*buf)[:readSize])
	return
}
