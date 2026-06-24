package signer

import "github.com/vmihailenco/msgpack/v5"

// OrderedMap is an insertion-ordered string-keyed map that msgpack-encodes its
// entries in declaration order — the Python `dict` semantics that the action
// hash depends on. Go's built-in maps iterate randomly and MUST NOT be used for
// any value that feeds the signature.
//
// Production actions use the typed wires in internal/order; OrderedMap covers
// dynamic or test-only actions where a struct would be overkill.
type OrderedMap struct {
	pairs []omPair
}

type omPair struct {
	key string
	val any
}

// NewOrderedMap builds an OrderedMap from alternating key/value arguments.
func NewOrderedMap(kv ...any) *OrderedMap {
	m := &OrderedMap{}
	for i := 0; i+1 < len(kv); i += 2 {
		m.Set(kv[i].(string), kv[i+1])
	}
	return m
}

// Set appends a key/value pair, preserving order.
func (m *OrderedMap) Set(key string, val any) *OrderedMap {
	m.pairs = append(m.pairs, omPair{key: key, val: val})
	return m
}

// EncodeMsgpack implements msgpack.CustomEncoder: a map header followed by each
// key/value in insertion order.
func (m *OrderedMap) EncodeMsgpack(enc *msgpack.Encoder) error {
	if err := enc.EncodeMapLen(len(m.pairs)); err != nil {
		return err
	}
	for _, p := range m.pairs {
		if err := enc.EncodeString(p.key); err != nil {
			return err
		}
		if err := enc.Encode(p.val); err != nil {
			return err
		}
	}
	return nil
}
