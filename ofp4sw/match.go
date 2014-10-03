package ofp4sw

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/hkwi/gopenflow/ofp4"
	"hash/fnv"
	"log"
	"sort"
)

/*
AddOxmHandler registers this OxmHandler.
*/
type OxmHandler interface {
	// return true if target frame matches
	Match(frame []byte, oxm []byte) (bool, error)
	SetField(frame []byte, oxm []byte) ([]byte, error)

	// Fits returns true if this could be matched by oxm.
	// Arguments of narrow, wide is a serialized form of multiple oxm.
	Fit(narrow, wide []byte) (bool, error)
	// Conflict will be used to check OFPFMFC_OVERLAP
	Conflict(a, b []byte) (bool, error)

	// OxmId is called in creating experimenter oxm key (see table features section in spec)
	// This allows setting the maximum length for variable length experimenter oxm.
	// field will be 64 bits and payload will be omitted.
	OxmId(field []byte) ([]byte, error)

	// used to fill up prerequisite oxm matches
	Expand(fields []byte) ([]byte, error)
}

/*
oxm_type without oxm_has_mask and oxm_length for OFPXMC_EXPERIMENTER.
*/
var oxmHandlers map[oxmExperimenterKey]OxmHandler = make(map[oxmExperimenterKey]OxmHandler)

func AddOxmHandler(oxmType uint32, experimenter uint32, handle OxmHandler) {
	if ofp4.OxmHeader(oxmType).Class() != ofp4.OFPXMC_EXPERIMENTER {
		panic("oxmType must have OFPXMC_EXPERIMENTER class")
	}
	oxmHandlers[oxmExperimenterKey{
		Type:         ofp4.OxmHeader(oxmType).Type(), // mask-out HasMask and Length
		Experimenter: experimenter,
	}] = handle
}

var oxmOfbAll []uint32 = []uint32{
	ofp4.OXM_OF_IN_PORT,
	ofp4.OXM_OF_IN_PHY_PORT,
	ofp4.OXM_OF_METADATA,
	ofp4.OXM_OF_ETH_DST,
	ofp4.OXM_OF_ETH_SRC,
	ofp4.OXM_OF_ETH_TYPE,
	ofp4.OXM_OF_VLAN_VID,
	ofp4.OXM_OF_VLAN_PCP,
	ofp4.OXM_OF_IP_DSCP,
	ofp4.OXM_OF_IP_ECN,
	ofp4.OXM_OF_IP_PROTO,
	ofp4.OXM_OF_IPV4_SRC,
	ofp4.OXM_OF_IPV4_DST,
	ofp4.OXM_OF_TCP_SRC,
	ofp4.OXM_OF_TCP_DST,
	ofp4.OXM_OF_UDP_SRC,
	ofp4.OXM_OF_UDP_DST,
	ofp4.OXM_OF_SCTP_SRC,
	ofp4.OXM_OF_SCTP_DST,
	ofp4.OXM_OF_ICMPV4_TYPE,
	ofp4.OXM_OF_ICMPV4_CODE,
	ofp4.OXM_OF_ARP_OP,
	ofp4.OXM_OF_ARP_SPA,
	ofp4.OXM_OF_ARP_TPA,
	ofp4.OXM_OF_ARP_SHA,
	ofp4.OXM_OF_ARP_THA,
	ofp4.OXM_OF_IPV6_SRC,
	ofp4.OXM_OF_IPV6_DST,
	ofp4.OXM_OF_IPV6_FLABEL,
	ofp4.OXM_OF_ICMPV6_TYPE,
	ofp4.OXM_OF_ICMPV6_CODE,
	ofp4.OXM_OF_IPV6_ND_TARGET,
	ofp4.OXM_OF_IPV6_ND_SLL,
	ofp4.OXM_OF_IPV6_ND_TLL,
	ofp4.OXM_OF_MPLS_LABEL,
	ofp4.OXM_OF_MPLS_TC,
	ofp4.OXM_OF_MPLS_BOS,
	ofp4.OXM_OF_PBB_ISID,
	ofp4.OXM_OF_TUNNEL_ID,
	ofp4.OXM_OF_IPV6_EXTHDR,
}

func oxmBasicPrereq(oxmType uint32) *oxmBasic {
	var ext *oxmBasic
	switch oxmType {
	case ofp4.OXM_OF_IPV4_SRC, ofp4.OXM_OF_IPV4_DST:
		ext = &oxmBasic{
			ofp4.OXM_OF_ETH_TYPE,
			[]byte{0x80, 0x00},
			[]byte{0xFF, 0xFF},
		}
	case ofp4.OXM_OF_TCP_SRC, ofp4.OXM_OF_TCP_DST:
		ext = &oxmBasic{
			ofp4.OXM_OF_IP_PROTO,
			[]byte{0x06},
			[]byte{0xFF},
		}
	case ofp4.OXM_OF_UDP_SRC, ofp4.OXM_OF_UDP_DST:
		ext = &oxmBasic{
			ofp4.OXM_OF_IP_PROTO,
			[]byte{0x11},
			[]byte{0xFF},
		}
	case ofp4.OXM_OF_SCTP_SRC, ofp4.OXM_OF_SCTP_DST:
		ext = &oxmBasic{
			ofp4.OXM_OF_IP_PROTO,
			[]byte{0x84},
			[]byte{0xFF},
		}
	case ofp4.OXM_OF_ICMPV4_TYPE, ofp4.OXM_OF_ICMPV4_CODE:
		ext = &oxmBasic{
			ofp4.OXM_OF_IP_PROTO,
			[]byte{0x01},
			[]byte{0xFF},
		}
	case ofp4.OXM_OF_ARP_OP,
		ofp4.OXM_OF_ARP_SPA, ofp4.OXM_OF_ARP_TPA,
		ofp4.OXM_OF_ARP_SHA, ofp4.OXM_OF_ARP_THA:
		ext = &oxmBasic{
			ofp4.OXM_OF_ETH_TYPE,
			[]byte{0x08, 0x06},
			[]byte{0xFF, 0xFF},
		}
	case ofp4.OXM_OF_IPV6_SRC, ofp4.OXM_OF_IPV6_DST, ofp4.OXM_OF_IPV6_FLABEL:
		ext = &oxmBasic{
			ofp4.OXM_OF_ETH_TYPE,
			[]byte{0x86, 0xDD},
			[]byte{0xFF, 0xFF},
		}
	case ofp4.OXM_OF_ICMPV6_TYPE, ofp4.OXM_OF_ICMPV6_CODE:
		ext = &oxmBasic{
			ofp4.OXM_OF_IP_PROTO,
			[]byte{0x3A},
			[]byte{0xFF},
		}
	case ofp4.OXM_OF_IPV6_ND_SLL:
		ext = &oxmBasic{
			ofp4.OXM_OF_ICMPV6_TYPE,
			[]byte{135},
			[]byte{0xFF},
		}
	case ofp4.OXM_OF_IPV6_ND_TLL:
		ext = &oxmBasic{
			ofp4.OXM_OF_ICMPV6_TYPE,
			[]byte{136},
			[]byte{0xFF},
		}
	case ofp4.OXM_OF_PBB_ISID:
		ext = &oxmBasic{
			ofp4.OXM_OF_ETH_TYPE,
			[]byte{0x88, 0xE7},
			[]byte{0xFF, 0xFF},
		}
	case ofp4.OXM_OF_IPV6_EXTHDR:
		ext = &oxmBasic{
			ofp4.OXM_OF_ETH_TYPE,
			[]byte{0x86, 0xDD},
			[]byte{0xFF, 0xFF},
		}
	}
	return ext
}

func OxmSplit(msg []byte) (oxms [][]byte) {
	for len(msg) > 4 {
		mt := ofp4.OxmHeader(binary.BigEndian.Uint32(msg))
		if mt == 0 { // this happens at OFPAT_SET_FIELD padding
			break
		}
		length := mt.Length()
		oxms = append(oxms, msg[:4+length])
		msg = msg[4+length:]
	}
	return
}

// basicMatch represents OFPMT_OXM + OFPXMC_OPENFLOW_BASIC series.
type oxmBasic struct {
	Type  uint32 // mask and length must be masked-out
	Value []byte
	// 0 means don't care
	Mask []byte // nil if has_mask==0
}

func (self oxmBasic) Match(data frame) bool {
	value, err := data.getValue(self.Type)
	if err != nil {
		return false
	}
	return bytes.Equal(maskBytes(value, self.Mask), self.Value)
}

func (self oxmBasic) Fit(spec oxmBasic) bool {
	if self.Type != spec.Type {
		return false // should not happen, caller should check.
	}
	return bytes.Equal(maskBytes(spec.Value, self.Mask), self.Value)
}

func (self oxmBasic) Conflict(target oxmBasic) bool {
	if self.Type != target.Type {
		return false // should not happen, caller should check.
	}
	mask := maskBytes(self.Mask, target.Mask)
	return !bytes.Equal(maskBytes(self.Value, mask), maskBytes(target.Value, mask))
}

func (self oxmBasic) MarshalBinary() ([]byte, error) {
	header := ofp4.OxmHeader(self.Type)
	var buf []byte
	if length, mayMask := ofp4.OxmOfDefs(self.Type); length == 0 {
		return nil, fmt.Errorf("unknown oxm basic")
	} else if mayMask && self.Mask != nil {
		header.SetMask(true)
		header.SetLength(length * 2)
		buf = make([]byte, 4+length*2)
		copy(buf[4+length:], self.Mask)
	} else {
		header.SetMask(false)
		header.SetLength(length)
		buf = make([]byte, 4+length)
	}
	binary.BigEndian.PutUint32(buf, uint32(header))
	copy(buf[4:], self.Value)
	return buf, nil
}

func (self oxmBasic) Equal(target oxmBasic) bool {
	if self.Type != target.Type ||
		!bytes.Equal(self.Value, target.Value) ||
		!bytes.Equal(self.Mask, target.Mask) {
		return false
	}
	return true
}

type oxmExperimenterKey struct {
	Type         uint32 // experimenter may define field
	Experimenter uint32
}

type oxmExperimenter struct {
	oxmExperimenterKey
	Data []byte
}

func (self oxmExperimenter) MarshalBinary() ([]byte, error) {
	mt := ofp4.OxmHeader(self.Type)
	mt.SetLength(4 + len(self.Data))
	buf := make([]byte, 8+len(self.Data))
	binary.BigEndian.PutUint32(buf, uint32(mt))
	binary.BigEndian.PutUint32(buf[4:], self.Experimenter)
	copy(buf[8:], self.Data)
	return buf, nil
}

func (self oxmExperimenter) Equal(target oxmExperimenter) bool {
	if self.oxmExperimenterKey != target.oxmExperimenterKey {
		return false
	}
	return bytes.Equal(self.Data, target.Data)
}

type match struct {
	basic []oxmBasic
	exp   map[oxmExperimenterKey][]byte // value may contain multiple oxmtlv.
}

func (self match) Match(data frame) bool {
	for _, m := range self.basic {
		if !m.Match(data) {
			return false
		}
	}
	if len(self.exp) > 0 {
		if buf, err := data.data(); err != nil {
			return false
		} else {
			for k, oxm := range self.exp {
				if handler, ok := oxmHandlers[k]; ok {
					if result, err := handler.Match(buf, oxm); err != nil {
						log.Print(err)
						return false
					} else if !result {
						return false
					}
				}
			}
		}
	}
	return true
}

func (self match) isEmpty() bool {
	return len(self.basic) == 0 && len(self.exp) == 0
}

/*
Returned value will be true if self in the flow table matches target in openflow query message.
You must pass expanded match.
*/
func (self match) Fit(target match) (bool, error) {
	for _, t := range target.basic {
		for _, s := range self.basic {
			if t.Type == s.Type {
				if !s.Fit(t) {
					return false, nil
				}
			}
		}
	}
	if tt, err := target.MarshalBinary(); err != nil {
		return false, err
	} else {
		for field, ss := range self.exp {
			if handler, ok := oxmHandlers[field]; ok {
				if result, err := handler.Fit(ss, tt); err != nil {
					return false, err
				} else if !result {
					return false, err
				}
			} else {
				return false, ofp4.Error{
					Type: ofp4.OFPET_BAD_MATCH,
					Code: ofp4.OFPBMC_BAD_TYPE,
				}
			}
		}
	}
	return true, nil
}

/*
You should pass expanded match.
*/
func (self match) Conflict(target match) (bool, error) {
	for _, t := range target.basic {
		for _, s := range self.basic {
			if s.Type == t.Type {
				if s.Conflict(t) {
					return true, nil
				}
			}
		}
	}
	for k, tt := range target.exp {
		if ss, ok := self.exp[k]; ok {
			if handle, ok := oxmHandlers[k]; ok {
				if result, err := handle.Conflict(ss, tt); err != nil {
					return false, err
				} else if result {
					return true, nil
				}
			} else {
				return false, ofp4.Error{
					Type: ofp4.OFPET_BAD_MATCH,
					Code: ofp4.OFPBMC_BAD_TYPE,
				}
			}
		}
	}
	return false, nil
}

func (self match) MarshalBinary() ([]byte, error) {
	var ret []byte
	for _, b := range self.basic {
		if buf, err := b.MarshalBinary(); err != nil {
			return nil, err
		} else {
			ret = append(ret, buf...)
		}
	}
	for _, e := range self.exp {
		ret = append(ret, e...)
	}
	return ret, nil
}

func (self *match) UnmarshalBinary(msg []byte) error {
	for _, oxmtlv := range OxmSplit(msg) {
		mt := ofp4.OxmHeader(binary.BigEndian.Uint32(oxmtlv))
		if mt == 0 { // this happens at OFPAT_SET_FIELD padding
			break
		}

		switch mt.Class() {
		case ofp4.OFPXMC_OPENFLOW_BASIC:
			m := oxmBasic{
				Type: mt.Type(),
			}
			if mt.HasMask() {
				length := mt.Length() / 2
				m.Value = oxmtlv[4 : 4+length]
				m.Mask = oxmtlv[4+length:]
			} else {
				m.Value = oxmtlv[4:]
			}
			self.basic = append(self.basic, m)
		case ofp4.OFPXMC_EXPERIMENTER:
			if len(oxmtlv) < 8 {
				return ofp4.Error{
					Code: ofp4.OFPET_BAD_MATCH,
					Type: ofp4.OFPBRC_BAD_LEN,
				}
			}
			key := oxmExperimenterKey{
				Type:         mt.Type(),
				Experimenter: binary.BigEndian.Uint32(oxmtlv[4:]),
			}
			self.exp[key] = append(self.exp[key], oxmtlv...)
		default:
			return ofp4.Error{
				Type: ofp4.OFPET_BAD_MATCH,
				Code: ofp4.OFPBMC_BAD_TYPE,
			}
		}
	}
	return nil
}

func (self match) Expand() (match, error) {
	var ret match
	basicMap := make(map[uint32]oxmBasic)
	expMap := make(map[oxmExperimenterKey]BytesSet)

	addBasic := func(b oxmBasic) error {
		k := b.Type
		if v, ok := basicMap[k]; !ok {
			basicMap[k] = b
		} else {
			if !v.Equal(b) { // conflicts
				return ofp4.Error{
					Type: ofp4.OFPET_BAD_MATCH,
					Code: ofp4.OFPBMC_BAD_VALUE,
				}
			}
		}
		return nil
	}

	for _, b := range self.basic {
		if err := addBasic(b); err != nil {
			return match{}, err
		}
	}

	for key, tlvIn := range self.exp {
		handler, ok := oxmHandlers[key]
		if !ok {
			return match{}, ofp4.Error{
				Type: ofp4.OFPET_BAD_MATCH,
				Code: ofp4.OFPBMC_BAD_TYPE,
			}
		}

		var full match
		if tlvOut, err := handler.Expand(tlvIn); err != nil {
			return match{}, err
		} else if err := full.UnmarshalBinary(tlvOut); err != nil {
			return match{}, err
		} else {
			for _, b := range full.basic {
				if err := addBasic(b); err != nil {
					return match{}, err
				}
			}
			for k, tlvs := range full.exp {
				for _, tlv := range OxmSplit(tlvs) {
					c := expMap[k]
					c.Add(tlv)
					expMap[k] = c
				}
			}
		}
	}

	var basic []oxmBasic
	for _, v := range basicMap {
		basic = append(basic, v)
	}
	ret.basic = oxmBasicList(basic).Expand()
	for k, hash := range expMap {
		var buf []byte
		for _, v := range hash {
			buf = append(buf, v...)
		}
		ret.exp[k] = buf
	}
	return ret, nil
}

/*
You must pass expanded match.
*/
func (self match) Equal(target match) (bool, error) {
	if ss, err := self.Expand(); err != nil {
		return false, err
	} else if tt, err := target.Expand(); err != nil {
		return false, err
	} else {
		if r1, err := ss.Fit(tt); err != nil {
			return false, err
		} else if r2, err := tt.Fit(ss); err != nil {
			return false, err
		} else if r1 && r2 {
			return true, nil
		}
		return false, nil
	}
}

// oxmBasicUnion creates a common match union parameter.
func oxmBasicUnion(f1, f2 []oxmBasic) []oxmBasic {
	var ret []oxmBasic
	for _, m1 := range f1 {
		for _, m2 := range f2 {
			if m1.Type == m2.Type {
				length := IntMin(len(m1.Value), len(m2.Value))
				value := make([]byte, length)
				mask := make([]byte, length)

				maskFull := true
				for i, _ := range mask {
					mask[i] = 0xFF // exact value
					if m1.Mask != nil {
						mask[i] &= m1.Mask[i]
					}
					if m2.Mask != nil {
						mask[i] &= m2.Mask[i]
					}
					e1 := m1.Value[i] & mask[i]
					e2 := m2.Value[i] & mask[i]
					if e1 != e2 {
						mask[i] ^= e1 ^ e2
						value[i] = (e1 & e2) &^ (e1 ^ e2)
					}
					if mask[i] != 0 {
						maskFull = false
					}
				}
				if !maskFull {
					ret = append(ret, oxmBasic{
						Type:  m1.Type,
						Mask:  mask,
						Value: value,
					})
				}
			}
		}
	}
	return ret
}

func oxmBasicUnionKey(uni []oxmBasic, basic []oxmBasic) uint32 {
	hasher := fnv.New32()
	for _, u := range uni {
		for _, b := range basic {
			if u.Type == b.Type {
				var value []byte
				if u.Mask != nil {
					length := IntMin(len(b.Value), len(u.Mask))
					value = make([]byte, length)
					for i, _ := range value {
						value[i] = b.Value[i] & u.Mask[i]
					}
				} else {
					value = make([]byte, len(b.Value))
					copy(value, b.Value)
				}
				for cur := 0; cur < len(value); {
					n, err := hasher.Write(value)
					if err != nil {
						panic(err)
					}
					if n == 0 {
						break
					}
					cur += n
				}
			}
		}
	}
	return hasher.Sum32()
}

type oxmBasicList []oxmBasic

func (self oxmBasicList) Len() int {
	return len([]oxmBasic(self))
}

func (self oxmBasicList) Less(i, j int) bool {
	inner := []oxmBasic(self)
	if inner[i].Type != inner[j].Type {
		return inner[i].Type < inner[j].Type
	}
	if ofp4.OxmHeader(inner[i].Type).HasMask() {
		a := maskBytes(inner[i].Value, inner[i].Mask)
		b := maskBytes(inner[j].Value, inner[j].Mask)
		if vcmp := bytes.Compare(a, b); vcmp != 0 {
			return vcmp < 0
		}
	}
	if vcmp := bytes.Compare(inner[i].Value, inner[j].Value); vcmp != 0 {
		return vcmp < 0
	}
	if mcmp := bytes.Compare(inner[i].Mask, inner[j].Mask); mcmp != 0 {
		return mcmp < 0
	}
	return false
}

func (self oxmBasicList) Swap(i, j int) {
	inner := []oxmBasic(self)
	inner[i], inner[j] = inner[j], inner[i]
	return
}

/*
Expand creates a new expanded oxmBasicList. The list returned will be sorted.
*/
func (self oxmBasicList) Expand() []oxmBasic {
	x := make(map[uint32]*oxmBasic)
	for _, m := range []oxmBasic(self) {
		for h := &m; h != nil; h = oxmBasicPrereq(h.Type) {
			x[h.Type] = h
		}
	}
	u := make([]oxmBasic, 0, len(x))
	for _, m := range x {
		u = append(u, *m)
	}
	sort.Sort(oxmBasicList(u))
	return u
}