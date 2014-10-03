package ofp4sw

import (
	"encoding"
	"github.com/hkwi/gopenflow/ofp4"
)

// Experimenter instructions and actions are identified by (experimenter-id, experimenter-type) pair.
type experimenterKey struct {
	Id   uint32
	Type uint32
}

type experimenterProp struct {
	experimenterKey
	Data []byte
}

// Static types are
// 1) uint16 for OFPIT_*
// 2) experimenterKey
type instructionKey interface{}

type instructionKeyList []instructionKey

func (self instructionKeyList) Have(key instructionKey) bool {
	for _, k := range []instructionKey(self) {
		if k == key {
			return true
		}
	}
	return false
}

// Static types are
// 1) uint16 for OFPAT_*
// 2) experimenterKey
type actionKey interface{}

type actionKeyList []actionKey

func (self actionKeyList) Have(key actionKey) bool {
	for _, k := range []actionKey(self) {
		if k == key {
			return true
		}
	}
	return false
}

// Static types are
// 1) uint32 for OFPXMC_OPENFLOW_BASIC oxm field
// 2) oxmExperimenterKey
type oxmKey interface{}

type oxmKeyList []oxmKey

func (self oxmKeyList) Have(key oxmKey) bool {
	for _, k := range []oxmKey(self) {
		if k == key {
			return true
		}
	}
	return false
}

// special rule here. nil means "NOT SET"
type flowTableFeatureProps struct {
	inst          []instructionKey
	next          []uint8
	writeActions  []actionKey
	applyActions  []actionKey
	writeSetfield []oxmKey
	applySetfield []oxmKey
	experimenter  []experimenterProp
}

type flowTableFeature struct {
	name          string
	metadataMatch uint64 // manually initialize with 0xFFFFFFFFFFFFFFFF
	metadataWrite uint64 // manually initialize with 0xFFFFFFFFFFFFFFFF
	config        uint32
	maxEntries    uint32
	// properties
	match     []oxmKey
	wildcards []oxmKey
	hit       flowTableFeatureProps
	miss      flowTableFeatureProps
}

func (self *flowTableFeature) importProps(props []encoding.BinaryMarshaler) {
	for _, prop := range props {
		switch p := prop.(type) {
		case *ofp4.TableFeaturePropInstructions:
			ids := []instructionKey{}
			for _, instId := range p.InstructionIds {
				switch inst := instId.(type) {
				case *ofp4.InstructionId:
					ids = append(ids, inst.Type)
				case *ofp4.InstructionExperimenter:
					ids = append(ids, experimenterKey{
						Id:   inst.Experimenter,
						Type: inst.ExpType,
					})
				default:
					panic("unexpected")
				}
			}
			switch p.Type {
			case ofp4.OFPTFPT_INSTRUCTIONS:
				self.hit.inst = ids
			case ofp4.OFPTFPT_INSTRUCTIONS_MISS:
				self.miss.inst = ids
			default:
				panic("unexpected")
			}
		case *ofp4.TableFeaturePropNextTables:
			switch p.Type {
			case ofp4.OFPTFPT_NEXT_TABLES:
				self.hit.next = p.NextTableIds
			case ofp4.OFPTFPT_NEXT_TABLES_MISS:
				self.miss.next = p.NextTableIds
			default:
				panic("unexpected")
			}
		case *ofp4.TableFeaturePropActions:
			ids := []actionKey{}
			for _, act := range p.ActionIds {
				switch a := act.(type) {
				case *ofp4.ActionGeneric:
					ids = append(ids, a.Type)
				case *ofp4.ActionExperimenter:
					ids = append(ids, experimenterKey{
						Id:   a.Experimenter,
						Type: a.ExpType,
					})
				default:
					panic("unexpected")
				}
			}
			switch p.Type {
			case ofp4.OFPTFPT_WRITE_ACTIONS:
				self.hit.writeActions = ids
			case ofp4.OFPTFPT_WRITE_ACTIONS_MISS:
				self.miss.writeActions = ids
			case ofp4.OFPTFPT_APPLY_ACTIONS:
				self.hit.applyActions = ids
			case ofp4.OFPTFPT_APPLY_ACTIONS_MISS:
				self.miss.applyActions = ids
			default:
				panic("unexpected")
			}
		case *ofp4.TableFeaturePropOxm:
			ids := []oxmKey{}
			var exp *oxmExperimenterKey
			for _, v := range p.OxmIds {
				if exp == nil {
					if ofp4.OxmHeader(v).Class() == ofp4.OFPXMC_EXPERIMENTER {
						exp = &oxmExperimenterKey{Type: v}
					} else {
						ids = append(ids, v)
					}
				} else {
					exp.Experimenter = v
					ids = append(ids, *exp)
					exp = nil
				}
			}
			switch p.Type {
			case ofp4.OFPTFPT_MATCH:
				self.match = ids
			case ofp4.OFPTFPT_WILDCARDS:
				self.wildcards = ids
			case ofp4.OFPTFPT_WRITE_SETFIELD:
				self.hit.writeSetfield = ids
			case ofp4.OFPTFPT_WRITE_SETFIELD_MISS:
				self.miss.writeSetfield = ids
			case ofp4.OFPTFPT_APPLY_SETFIELD:
				self.hit.applySetfield = ids
			case ofp4.OFPTFPT_APPLY_SETFIELD_MISS:
				self.miss.applySetfield = ids
			default:
				panic("unexpected")
			}
		case *ofp4.TableFeaturePropExperimenter:
			exp := experimenterProp{
				experimenterKey: experimenterKey{
					Id:   p.Experimenter,
					Type: p.ExpType,
				},
				Data: p.Data,
			}
			switch p.Type {
			case ofp4.OFPTFPT_EXPERIMENTER:
				self.hit.experimenter = append(self.hit.experimenter, exp)
			case ofp4.OFPTFPT_EXPERIMENTER_MISS:
				self.miss.experimenter = append(self.hit.experimenter, exp)
			default:
				panic("unexpected")
			}
		}
	}
}

// See openflow switch 1.3.4 spec "Flow Table Modification Messages" page 40
func (self flowTableFeature) accepts(entry *flowEntry, priority uint16) error {
	isTableMiss := false
	if entry.fields.isEmpty() && priority == 0 {
		isTableMiss = true
	}

	var instKeys instructionKeyList
	if isTableMiss && self.miss.inst != nil {
		instKeys = instructionKeyList(self.miss.inst)
	} else if self.hit.inst != nil {
		instKeys = instructionKeyList(self.hit.inst)
	}

	if entry.instGoto != 0 {
		if !instKeys.Have(uint16(ofp4.OFPIT_GOTO_TABLE)) {
			return ofp4.Error{
				Type: ofp4.OFPET_BAD_INSTRUCTION,
				Code: ofp4.OFPBIC_UNSUP_INST,
			}
		}

		var next []uint8
		if isTableMiss && self.miss.next != nil {
			next = self.miss.next
		} else if self.hit.next != nil {
			next = self.hit.next
		}
		if next != nil {
			supported := false
			for _, tableId := range next {
				if entry.instGoto == tableId {
					supported = true
				}
			}
			if !supported {
				return ofp4.Error{
					Type: ofp4.OFPET_BAD_INSTRUCTION,
					Code: ofp4.OFPBIC_BAD_TABLE_ID,
				}
			}
		}
	}
	if entry.instMetadata != nil {
		if !instKeys.Have(uint16(ofp4.OFPIT_WRITE_METADATA)) {
			return ofp4.Error{
				Type: ofp4.OFPET_BAD_INSTRUCTION,
				Code: ofp4.OFPBIC_UNSUP_INST,
			}
		}
		if entry.instMetadata.metadata&^self.metadataWrite != 0 {
			return ofp4.Error{
				Type: ofp4.OFPET_BAD_INSTRUCTION,
				Code: ofp4.OFPBIC_UNSUP_METADATA,
			}
		}
		if entry.instMetadata.mask&^self.metadataWrite != 0 {
			return ofp4.Error{
				Type: ofp4.OFPET_BAD_INSTRUCTION,
				Code: ofp4.OFPBIC_UNSUP_METADATA_MASK,
			}
		}
	}
	if !isTableMiss && self.match != nil {
		specified := make(map[oxmKey]bool)
		for _, k := range self.match {
			specified[k] = false
		}
		for _, m := range entry.fields.basic {
			if !oxmKeyList(self.match).Have(m.Type) {
				return ofp4.Error{
					Type: ofp4.OFPET_BAD_MATCH,
					Code: ofp4.OFPBMC_BAD_FIELD,
				}
			}
			if specified[m.Type] {
				return ofp4.Error{
					Type: ofp4.OFPET_BAD_MATCH,
					Code: ofp4.OFPBMC_DUP_FIELD,
				}
			} else {
				specified[m.Type] = true
			}
		}
		for key, _ := range entry.fields.exp {
			if !oxmKeyList(self.match).Have(key) {
				return ofp4.Error{
					Type: ofp4.OFPET_BAD_MATCH,
					Code: ofp4.OFPBMC_BAD_FIELD,
				}
			}
			specified[key] = true
		}
		for _, k := range self.wildcards {
			specified[k] = true
		}
		for _, v := range specified {
			if !v {
				return ofp4.Error{
					Type: ofp4.OFPET_BAD_MATCH,
					Code: ofp4.OFPBMC_BAD_WILDCARDS,
				}
			}
		}
	}

	if len([]action(entry.instApply)) > 0 {
		if !instKeys.Have(uint16(ofp4.OFPIT_APPLY_ACTIONS)) {
			return ofp4.Error{
				Type: ofp4.OFPET_BAD_INSTRUCTION,
				Code: ofp4.OFPBIC_UNSUP_INST,
			}
		}
		var keys []actionKey
		if isTableMiss && self.miss.applyActions != nil {
			keys = self.miss.applyActions
		} else if self.hit.applyActions != nil {
			keys = self.hit.applyActions
		}
		if keys != nil {
			for _, act := range []action(entry.instApply) {
				var aKey actionKey
				switch a := act.(type) {
				case *actionExperimenter:
					aKey = a.experimenterKey
				case *actionOutput:
					aKey = uint16(ofp4.OFPAT_OUTPUT)
				case *actionMplsTtl:
					aKey = uint16(ofp4.OFPAT_SET_MPLS_TTL)
				case *actionPush:
					aKey = a.Type
				case *actionPopMpls:
					aKey = uint16(ofp4.OFPAT_POP_MPLS)
				case *actionSetQueue:
					aKey = uint16(ofp4.OFPAT_SET_QUEUE)
				case *actionGroup:
					aKey = uint16(ofp4.OFPAT_GROUP)
				case *actionNwTtl:
					aKey = uint16(ofp4.OFPAT_SET_NW_TTL)
				case *actionSetField:
					aKey = uint16(ofp4.OFPAT_SET_FIELD)
				}
				if !actionKeyList(keys).Have(aKey) {
					return ofp4.Error{
						Type: ofp4.OFPET_BAD_ACTION,
						Code: ofp4.OFPBAC_BAD_TYPE,
					}
				}
			}
			// XXX: Experimenter
		}
	}
	if entry.instWrite.Len() > 0 {
		if !instKeys.Have(uint16(ofp4.OFPIT_WRITE_ACTIONS)) {
			return ofp4.Error{
				Type: ofp4.OFPET_BAD_INSTRUCTION,
				Code: ofp4.OFPBIC_UNSUP_INST,
			}
		}
		var keys []actionKey
		if isTableMiss && self.miss.writeActions != nil {
			keys = self.miss.writeActions
		} else if self.hit.writeActions != nil {
			keys = self.hit.writeActions
		}
		if keys != nil {
			for _, a := range entry.instWrite.hash {
				if !actionKeyList(keys).Have(a.Key()) {
					return ofp4.Error{
						Type: ofp4.OFPET_BAD_ACTION,
						Code: ofp4.OFPBAC_BAD_TYPE,
					}
				}
			}
			for k, _ := range entry.instWrite.exp {
				if !actionKeyList(keys).Have(k) {
					return ofp4.Error{
						Type: ofp4.OFPET_BAD_ACTION,
						Code: ofp4.OFPBAC_BAD_TYPE,
					}
				}
			}
		}
	}
	for _, insts := range entry.instExp {
		for _, inst := range insts {
			if !instKeys.Have(inst.experimenterKey) {
				return ofp4.Error{
					Type: ofp4.OFPET_BAD_INSTRUCTION,
					Code: ofp4.OFPBIC_UNSUP_INST,
				}
			}
		}
	}
	if entry.instMeter != 0 {
		if !instKeys.Have(uint16(ofp4.OFPIT_METER)) {
			return ofp4.Error{
				Type: ofp4.OFPET_BAD_INSTRUCTION,
				Code: ofp4.OFPBIC_UNSUP_INST,
			}
		}
	}

	return nil
}