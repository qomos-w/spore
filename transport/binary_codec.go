package transport

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

const (
	// binaryMagic is the fixed frame preamble shared by every wire version.
	// The format version is deliberately NOT baked into the magic: it lives in
	// its own header byte (binaryWireVersion) so the wire format can evolve
	// without redefining the magic bytes.
	binaryMagic = "TBC"

	// binaryWireVersion is the current frame format version, written as the
	// byte immediately after binaryMagic. It is the single explicit source of
	// truth for whether a frame is accepted; the decoder never infers the
	// format from payload shape.
	binaryWireVersion byte = 3

	// binaryLegacyWireVersion is the version that used to be glued onto the
	// magic ("TBC\x02"). It is recognised only so the decoder can reject it
	// with a precise error: the only TBC consumer in-tree is the same-binary
	// child-actor response path, so no legacy frames are ever in flight.
	binaryLegacyWireVersion byte = 2

	// entrySequenceWireVersion is the first wire version whose format defines
	// the TAG_ENTRY_SEQUENCE container for map-kind scopes.
	entrySequenceWireVersion byte = 2

	// binaryHeaderLen is the full header size: the magic preamble plus the
	// explicit version byte.
	binaryHeaderLen = len(binaryMagic) + 1

	binaryTagNull          byte = 0x00
	binaryTagBoolFalse     byte = 0x01
	binaryTagBoolTrue      byte = 0x02
	binaryTagInt32         byte = 0x03
	binaryTagInt64         byte = 0x04
	binaryTagUint64        byte = 0x05
	binaryTagFloat64       byte = 0x06
	binaryTagString        byte = 0x07
	binaryTagBytes         byte = 0x08
	binaryTagArray         byte = 0x09
	binaryTagMap           byte = 0x0A
	binaryTagEntrySequence byte = 0x0B
	binaryTagStruct        byte = 0x0C
)

// BinaryCodec is a Codec implementation that uses a schema-aware custom binary format.
// It preserves the same canonical projection shapes as JSONCodec while producing
// a deterministic binary payload.
//
// Frame layout: the fixed 3-byte magic "TBC", then one explicit wire-version
// byte (binaryWireVersion), then a self-describing tag stream. The magic and
// the version are independent: the version is the sole determinant of frame
// acceptance, so the format can evolve without redefining the magic.
type BinaryCodec struct{}

func (c *BinaryCodec) Encode(s schema.TypeDesc, id identity.CanonicalID, value any) (View, error) {
	if err := validateEncodeType(s, value); err != nil {
		return View{}, encodeErrorf(s, id, "", err)
	}

	projected, err := binaryProjectValue(s, value)
	if err != nil {
		return View{}, encodeErrorf(s, id, "", err)
	}

	enc := &binaryEncoder{sd: s}
	enc.writeBytes([]byte(binaryMagic))
	enc.writeByte(binaryWireVersion)
	if err := enc.encodeValue(projected, nil); err != nil {
		return View{}, encodeErrorf(s, id, "", fmt.Errorf("binary encode: %w", err))
	}

	return View{
		Kind:     ViewKindFull,
		Schema:   s,
		Identity: id,
		Data:     enc.buf,
	}, nil
}

// parseBinaryHeader validates the fixed magic preamble and the explicit wire
// version, returning the payload that follows the header. Frame acceptance is
// decided by the declared version byte alone — there is no payload-shape
// sniffing on the read path.
func parseBinaryHeader(data []byte) ([]byte, error) {
	if len(data) < binaryHeaderLen {
		return nil, fmt.Errorf("binary decode: truncated header")
	}
	if string(data[:len(binaryMagic)]) != binaryMagic {
		return nil, fmt.Errorf("binary decode: invalid magic")
	}
	version := data[len(binaryMagic)]
	switch version {
	case binaryWireVersion:
		return data[binaryHeaderLen:], nil
	case binaryLegacyWireVersion:
		return nil, fmt.Errorf("binary decode: unsupported legacy wire version %d (want %d)", version, binaryWireVersion)
	default:
		return nil, fmt.Errorf("binary decode: unsupported wire version %d (want %d)", version, binaryWireVersion)
	}
}

// wireVersionSupportsEntrySequence reports whether the current wire version
// defines the TAG_ENTRY_SEQUENCE container for map-kind scopes. This replaces
// the former looksLikeEntrySequence runtime-shape heuristic: the format
// capability is determined by the declared wire version, and the schema kind
// (not the value's shape) selects the container.
func wireVersionSupportsEntrySequence() bool {
	return binaryWireVersion >= entrySequenceWireVersion
}

// DecodeInto decodes binary data directly into target using reflection,
// bypassing the generic map[string]any intermediate form for struct types.
// target must be a non-nil pointer.
func (c *BinaryCodec) DecodeInto(view View, target any) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return decodeErrorWithPath(view, "", "binary DecodeInto: target must be a non-nil pointer")
	}
	if len(view.Data) == 0 {
		return decodeErrorWithPath(view, "", "binary decode: empty data")
	}
	payload, err := parseBinaryHeader(view.Data)
	if err != nil {
		return decodeErrorWithPath(view, "", "%v", err)
	}
	dec := &binaryDecoder{data: payload, sd: view.Schema}
	if err := dec.decodeInto(rv.Elem(), ""); err != nil {
		return decodeErrorWithPath(view, "", "binary DecodeInto: %w", err)
	}
	if !dec.done() {
		return decodeErrorWithPath(view, "", "binary DecodeInto: trailing bytes")
	}
	return nil
}

func (c *BinaryCodec) Decode(view View) (any, error) {
	if len(view.Data) == 0 {
		return nil, &DecodeError{
			SchemaName: view.Schema.Name,
			Path:       "",
			Identity:   view.Identity,
			Err:        fmt.Errorf("empty data"),
		}
	}
	if len(view.Data) < binaryHeaderLen {
		return nil, decodeErrorWithPath(view, "", "binary decode: truncated header")
	}
	payload, err := parseBinaryHeader(view.Data)
	if err != nil {
		return nil, decodeErrorWithPath(view, "", "%v", err)
	}

	dec := &binaryDecoder{data: payload, sd: view.Schema}
	value, err := dec.decodeValue("")
	if err != nil {
		return nil, decodeErrorWithPath(view, "", "binary decode: %w", err)
	}
	if !dec.done() {
		return nil, decodeErrorWithPath(view, "", "binary decode: trailing bytes")
	}
	return canonicalizeDecodedValue(view, value)
}

type binaryEncoder struct {
	buf []byte
	sd  schema.TypeDesc // schema context for current encode scope
}

func (e *binaryEncoder) writeByte(b byte) {
	e.buf = append(e.buf, b)
}

func (e *binaryEncoder) writeBytes(data []byte) {
	e.buf = append(e.buf, data...)
}

func (e *binaryEncoder) writeUvarint(n uint64) {
	var scratch [binary.MaxVarintLen64]byte
	count := binary.PutUvarint(scratch[:], n)
	e.writeBytes(scratch[:count])
}

func (e *binaryEncoder) writeUint32(v uint32) {
	var scratch [4]byte
	binary.BigEndian.PutUint32(scratch[:], v)
	e.writeBytes(scratch[:])
}

func (e *binaryEncoder) writeUint64(v uint64) {
	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], v)
	e.writeBytes(scratch[:])
}

func (e *binaryEncoder) writeString(s string) {
	e.writeUvarint(uint64(len(s)))
	e.writeBytes([]byte(s))
}

func (e *binaryEncoder) encodeValue(value any, fieldSD *schema.TypeDesc) error {
	if value == nil {
		e.writeByte(binaryTagNull)
		return nil
	}

	// Use field-level schema when provided (for nested struct ClassID).
	if fieldSD != nil {
		saved := e.sd
		defer func() { e.sd = saved }()
		e.sd = *fieldSD
	}
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			e.writeByte(binaryTagNull)
			return nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			e.writeByte(binaryTagBoolTrue)
		} else {
			e.writeByte(binaryTagBoolFalse)
		}
		return nil
	case reflect.Int8, reflect.Int16, reflect.Int32:
		e.writeByte(binaryTagInt32)
		e.writeUint32(uint32(int32(v.Int())))
		return nil
	case reflect.Int, reflect.Int64:
		e.writeByte(binaryTagInt64)
		e.writeUint64(uint64(v.Int()))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		e.writeByte(binaryTagUint64)
		e.writeUint64(v.Uint())
		return nil
	case reflect.Float32, reflect.Float64:
		e.writeByte(binaryTagFloat64)
		e.writeUint64(math.Float64bits(v.Convert(reflect.TypeOf(float64(0))).Float()))
		return nil
	case reflect.String:
		e.writeByte(binaryTagString)
		e.writeString(v.String())
		return nil
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			e.writeByte(binaryTagBytes)
			e.writeUvarint(uint64(v.Len()))
			e.writeBytes(v.Bytes())
			return nil
		}
		fallthrough
	case reflect.Array:
		// Entry-sequence encoding is the canonical wire form for a map-kind
		// scope. The container is selected by an explicit format rule — the
		// declared wire version supports entry sequences and the schema kind is
		// a map — never by inspecting the runtime shape of the value. An
		// array<Struct> whose elements merely expose Key/Value fields has a
		// non-map scope and therefore stays a TAG_ARRAY of full structs.
		if e.sd.Kind == schema.TypeKindMap && wireVersionSupportsEntrySequence() {
			e.writeByte(binaryTagEntrySequence)
			e.writeUvarint(uint64(v.Len()))
			for i := 0; i < v.Len(); i++ {
				key, val, ok := entryFromProjectedValue(v.Index(i).Interface())
				if !ok {
					return fmt.Errorf("invalid entry sequence element at index %d", i)
				}
				if err := e.encodeValue(key, nil); err != nil {
					return err
				}
				if err := e.encodeValue(val, nil); err != nil {
					return err
				}
			}
			return nil
		}
		e.writeByte(binaryTagArray)
		e.writeUvarint(uint64(v.Len()))
		for i := 0; i < v.Len(); i++ {
			if err := e.encodeValue(v.Index(i).Interface(), nil); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("unsupported map key kind %s", v.Type().Key().Kind())
		}
		e.writeByte(binaryTagMap)
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		e.writeUvarint(uint64(len(keys)))
		for _, key := range keys {
			e.writeString(key.String())
			if err := e.encodeValue(v.MapIndex(key).Interface(), nil); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		if isOrderedMapValue(v) {
			entries, err := orderedMapEntries(v)
			if err != nil {
				return fmt.Errorf("ordered map projection: %w", err)
			}
			// An OrderedMap is a map by construction, at any nesting depth —
			// scope the entry-sequence encoding accordingly.
			return e.encodeValue(entries, &schema.TypeDesc{Kind: schema.TypeKindMap})
		}
		t := v.Type()

		// Collect exported fields sorted by wire name (alphabetical).
		type fieldEntry struct {
			wireName string
			value    any
		}
		var fields []fieldEntry
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			fields = append(fields, fieldEntry{
				wireName: jsonFieldName(field),
				value:    v.Field(i).Interface(),
			})
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].wireName < fields[j].wireName })

		// Snapshot the ClassID for this struct; nested structs inherit 0
		// (anonymous) unless fieldSD override provides one.
		classID := e.sd.ClassID
		savedSD := e.sd
		e.sd.ClassID = 0
		e.writeByte(binaryTagStruct)
		e.writeUvarint(uint64(classID))
		e.writeUvarint(uint64(len(fields)))

		if classID == 0 {
			// Anonymous struct: string-key fallback.
			for _, f := range fields {
				e.writeString(f.wireName)
				if err := e.encodeValue(f.value, nil); err != nil {
					return err
				}
			}
		} else {
			// Schema-typed struct: field-index encoding.
			for idx, f := range fields {
				e.writeUvarint(uint64(idx))
				if err := e.encodeValue(f.value, nil); err != nil {
					return err
				}
			}
		}
		e.sd = savedSD
		return nil
	default:
		return fmt.Errorf("unsupported value kind %s", v.Kind())
	}
}

type binaryDecoder struct {
	data []byte
	off  int
	sd   schema.TypeDesc
}

func (d *binaryDecoder) done() bool {
	return d.off == len(d.data)
}

func (d *binaryDecoder) readByte() (byte, error) {
	if d.off >= len(d.data) {
		return 0, fmt.Errorf("unexpected EOF")
	}
	b := d.data[d.off]
	d.off++
	return b, nil
}

func (d *binaryDecoder) readUvarint() (uint64, error) {
	value, count := binary.Uvarint(d.data[d.off:])
	if count == 0 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	if count < 0 {
		return 0, fmt.Errorf("invalid varint")
	}
	d.off += count
	return value, nil
}

func (d *binaryDecoder) readUint32() (uint32, error) {
	if len(d.data)-d.off < 4 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	value := binary.BigEndian.Uint32(d.data[d.off : d.off+4])
	d.off += 4
	return value, nil
}

func (d *binaryDecoder) readUint64() (uint64, error) {
	if len(d.data)-d.off < 8 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	value := binary.BigEndian.Uint64(d.data[d.off : d.off+8])
	d.off += 8
	return value, nil
}

func (d *binaryDecoder) readString() (string, error) {
	length, err := d.readUvarint()
	if err != nil {
		return "", err
	}
	if uint64(len(d.data)-d.off) < length {
		return "", fmt.Errorf("unexpected EOF")
	}
	value := string(d.data[d.off : d.off+int(length)])
	d.off += int(length)
	return value, nil
}

func (d *binaryDecoder) readBytes() ([]byte, error) {
	length, err := d.readUvarint()
	if err != nil {
		return nil, err
	}
	if uint64(len(d.data)-d.off) < length {
		return nil, fmt.Errorf("unexpected EOF")
	}
	value := append([]byte(nil), d.data[d.off:d.off+int(length)]...)
	d.off += int(length)
	return value, nil
}

// decodeInto reads a tagged value from the binary stream and populates
// target directly using reflection. Struct fields are populated by name
// matching against TAG_MAP keys — no intermediate map[string]any is created.
func (d *binaryDecoder) decodeInto(target reflect.Value, path string) error {
	tag, err := d.readByte()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if tag == binaryTagNull {
		return nil
	}
	return d.decodeTagInto(tag, target, path)
}

// decodeTagInto dispatches a (already-read) tag into the target reflect.Value.
func (d *binaryDecoder) decodeTagInto(tag byte, target reflect.Value, path string) error {
	for target.Kind() == reflect.Pointer {
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		target = target.Elem()
	}

	switch target.Kind() {
	case reflect.Struct:
		switch tag {
		case binaryTagMap:
			return d.decodeMapIntoStruct(target, path)
		case binaryTagStruct:
			return d.decodeStructByIndex(target, path)
		default:
			return fmt.Errorf("%s: expected TAG_MAP(0x0A) or TAG_STRUCT(0x0C) for struct, got 0x%02x", path, tag)
		}

	case reflect.Slice:
		if target.Type().Elem().Kind() == reflect.Uint8 {
			if tag != binaryTagBytes {
				return fmt.Errorf("%s: expected TAG_BYTES(0x08) for []byte, got 0x%02x", path, tag)
			}
			b, err := d.readBytes()
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			target.SetBytes(b)
			return nil
		}
		if tag == binaryTagArray {
			return d.decodeArrayIntoSlice(target, path)
		}
		if tag == binaryTagEntrySequence {
			return d.decodeEntrySeqIntoSlice(target, path)
		}
		return fmt.Errorf("%s: expected TAG_ARRAY(0x09) or TAG_ENTRYSEQ(0x0B) for slice, got 0x%02x", path, tag)

	case reflect.Map:
		if tag != binaryTagMap {
			return fmt.Errorf("%s: expected TAG_MAP(0x0A) for map, got 0x%02x", path, tag)
		}
		return d.decodeMapIntoMap(target, path)

	case reflect.String:
		if tag != binaryTagString {
			return fmt.Errorf("%s: expected TAG_STRING(0x07), got 0x%02x", path, tag)
		}
		s, err := d.readString()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		target.SetString(s)
		return nil

	case reflect.Bool:
		switch tag {
		case binaryTagBoolTrue:
			target.SetBool(true)
		case binaryTagBoolFalse:
			target.SetBool(false)
		default:
			return fmt.Errorf("%s: expected TAG_BOOL, got 0x%02x", path, tag)
		}
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch tag {
		case binaryTagInt32:
			v, err := d.readUint32()
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			target.SetInt(int64(int32(v)))
		case binaryTagInt64:
			v, err := d.readUint64()
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			target.SetInt(int64(v))
		case binaryTagUint64:
			v, err := d.readUint64()
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			target.SetInt(int64(v))
		default:
			return fmt.Errorf("%s: expected int tag, got 0x%02x", path, tag)
		}
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch tag {
		case binaryTagUint64:
			v, err := d.readUint64()
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			target.SetUint(v)
		case binaryTagInt32:
			v, err := d.readUint32()
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			target.SetUint(uint64(v))
		default:
			return fmt.Errorf("%s: expected uint tag, got 0x%02x", path, tag)
		}
		return nil

	case reflect.Float32, reflect.Float64:
		if tag != binaryTagFloat64 {
			return fmt.Errorf("%s: expected TAG_FLOAT64(0x06), got 0x%02x", path, tag)
		}
		v, err := d.readUint64()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		target.SetFloat(math.Float64frombits(v))
		return nil

	case reflect.Interface:
		// map[string]any struct fields (e.g. request Payload maps) decode
		// their values into interface{} targets: box the tagged value via the
		// generic dynamic decoder instead of failing on the target kind.
		v, err := d.decodeTaggedValue(tag, path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if v == nil {
			return nil
		}
		boxed := reflect.ValueOf(v)
		if !boxed.Type().AssignableTo(target.Type()) {
			return fmt.Errorf("%s: decoded %s not assignable to %s", path, boxed.Type(), target.Type())
		}
		target.Set(boxed)
		return nil

	default:
		return fmt.Errorf("%s: unsupported target kind %s", path, target.Kind())
	}
}

func (d *binaryDecoder) decodeMapIntoStruct(target reflect.Value, path string) error {
	count, err := d.readUvarint()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	// Build lookup from wire name (JSON tag or Go field name) → field index.
	t := target.Type()
	wireMap := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		wireMap[jsonFieldName(f)] = i
	}
	for i := uint64(0); i < count; i++ {
		key, err := d.readString()
		if err != nil {
			return fmt.Errorf("%s.field[%d]: %w", path, i, err)
		}
		idx, ok := wireMap[key]
		if !ok {
			// Unknown field — consume value and skip.
			if _, err := d.decodeValue(fmt.Sprintf("%s.%s", path, key)); err != nil {
				return err
			}
			continue
		}
		field := target.Field(idx)
		if !field.CanSet() {
			if _, err := d.decodeValue(fmt.Sprintf("%s.%s", path, key)); err != nil {
				return err
			}
			continue
		}
		if err := d.decodeInto(field, fmt.Sprintf("%s.%s", path, key)); err != nil {
			return err
		}
	}
	return nil
}

// decodeStructByIndex decodes a TAG_STRUCT payload into target using
// field-index addressing. Schema ID validation ensures wire schema matches
// the decoder's expected schema, returning an explicit error on mismatch.
func (d *binaryDecoder) decodeStructByIndex(target reflect.Value, path string) error {
	schemaID, err := d.readUvarint()
	if err != nil {
		return fmt.Errorf("%s: read schema id: %w", path, err)
	}
	count, err := d.readUvarint()
	if err != nil {
		return fmt.Errorf("%s: read field count: %w", path, err)
	}

	// Validate schema ID when both encoder and decoder carry one.
	if schemaID != 0 && d.sd.ClassID != 0 && schemaID != d.sd.ClassID {
		return fmt.Errorf("%s: schema mismatch: wire schemaID=%d, expected ClassID=%d", path, schemaID, d.sd.ClassID)
	}

	// Build sorted field list matching encoder order (alphabetical by wire name).
	t := target.Type()
	type fi struct {
		wireName string
		idx      int
	}
	var fields []fi
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fields = append(fields, fi{wireName: jsonFieldName(f), idx: i})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].wireName < fields[j].wireName })

	if schemaID == 0 {
		// Anonymous struct: string-key encoding.
		wireMap := make(map[string]int, len(fields))
		for _, f := range fields {
			wireMap[f.wireName] = f.idx
		}
		for i := uint64(0); i < count; i++ {
			key, err := d.readString()
			if err != nil {
				return fmt.Errorf("%s.field[%d]: %w", path, i, err)
			}
			fieldIdx, ok := wireMap[key]
			if !ok {
				if _, err := d.decodeValue(fmt.Sprintf("%s.%s", path, key)); err != nil {
					return err
				}
				continue
			}
			if err := d.decodeInto(target.Field(fieldIdx), fmt.Sprintf("%s.%s", path, key)); err != nil {
				return err
			}
		}
	} else {
		// Schema-typed: field-index encoding.
		for i := uint64(0); i < count; i++ {
			fieldIdx, err := d.readUvarint()
			if err != nil {
				return fmt.Errorf("%s.field[%d]: %w", path, i, err)
			}
			if int(fieldIdx) >= len(fields) {
				return fmt.Errorf("%s.field[%d]: field index %d out of range (max %d)", path, i, fieldIdx, len(fields)-1)
			}
			f := fields[fieldIdx]
			if err := d.decodeInto(target.Field(f.idx), fmt.Sprintf("%s.%s", path, f.wireName)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *binaryDecoder) decodeArrayIntoSlice(target reflect.Value, path string) error {
	count, err := d.readUvarint()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	elemType := target.Type().Elem()
	slice := reflect.MakeSlice(target.Type(), 0, int(count))
	for i := uint64(0); i < count; i++ {
		elem := reflect.New(elemType).Elem()
		if err := d.decodeInto(elem, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
		slice = reflect.Append(slice, elem)
	}
	target.Set(slice)
	return nil
}

func (d *binaryDecoder) decodeMapIntoMap(target reflect.Value, path string) error {
	count, err := d.readUvarint()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	result := reflect.MakeMapWithSize(target.Type(), int(count))
	valType := target.Type().Elem()
	for i := uint64(0); i < count; i++ {
		key, err := d.readString()
		if err != nil {
			return fmt.Errorf("%s.key[%d]: %w", path, i, err)
		}
		valVal := reflect.New(valType).Elem()
		if err := d.decodeInto(valVal, fmt.Sprintf("%s[%s]", path, key)); err != nil {
			return err
		}
		result.SetMapIndex(reflect.ValueOf(key), valVal)
	}
	target.Set(result)
	return nil
}

func (d *binaryDecoder) decodeEntrySeqIntoSlice(target reflect.Value, path string) error {
	count, err := d.readUvarint()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	elemType := target.Type().Elem()
	_, hasKey := elemType.FieldByName("Key")
	_, hasValue := elemType.FieldByName("Value")
	if !hasKey || !hasValue {
		return fmt.Errorf("%s: entry sequence target element must have Key and Value fields, got %s", path, elemType)
	}
	slice := reflect.MakeSlice(target.Type(), 0, int(count))
	for i := uint64(0); i < count; i++ {
		elem := reflect.New(elemType).Elem()
		// Entry sequence: each entry has a tagged key then a tagged value.
		entryPath := fmt.Sprintf("%s[%d]", path, i)
		if err := d.decodeInto(elem.FieldByName("Key"), entryPath+".Key"); err != nil {
			return err
		}
		if err := d.decodeInto(elem.FieldByName("Value"), entryPath+".Value"); err != nil {
			return err
		}
		slice = reflect.Append(slice, elem)
	}
	target.Set(slice)
	return nil
}

func (d *binaryDecoder) decodeValue(path string) (any, error) {
	tag, err := d.readByte()
	if err != nil {
		return nil, err
	}
	return d.decodeTaggedValue(tag, path)
}

// decodeTaggedValue boxes an already-read tag into a Go any. Shared by the
// generic Decode path (decodeValue) and decodeTagInto's Interface case,
// which receives its tag pre-read.
func (d *binaryDecoder) decodeTaggedValue(tag byte, path string) (any, error) {
	switch tag {
	case binaryTagNull:
		return nil, nil
	case binaryTagBoolFalse:
		return false, nil
	case binaryTagBoolTrue:
		return true, nil
	case binaryTagInt32:
		v, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return int32(v), nil
	case binaryTagInt64:
		v, err := d.readUint64()
		if err != nil {
			return nil, err
		}
		return int64(v), nil
	case binaryTagUint64:
		v, err := d.readUint64()
		if err != nil {
			return nil, err
		}
		return v, nil
	case binaryTagFloat64:
		v, err := d.readUint64()
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(v), nil
	case binaryTagString:
		return d.readString()
	case binaryTagBytes:
		return d.readBytes()
	case binaryTagArray:
		count, err := d.readUvarint()
		if err != nil {
			return nil, err
		}
		values := make([]any, count)
		for i := uint64(0); i < count; i++ {
			v, err := d.decodeValue(path)
			if err != nil {
				return nil, err
			}
			values[i] = v
		}
		return values, nil
	case binaryTagMap:
		count, err := d.readUvarint()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, count)
		for i := uint64(0); i < count; i++ {
			key, err := d.readString()
			if err != nil {
				return nil, err
			}
			val, err := d.decodeValue(path)
			if err != nil {
				return nil, err
			}
			m[key] = val
		}
		return m, nil
	case binaryTagEntrySequence:
		count, err := d.readUvarint()
		if err != nil {
			return nil, err
		}
		entries := make([]any, count)
		for i := uint64(0); i < count; i++ {
			key, err := d.decodeValue(path)
			if err != nil {
				return nil, err
			}
			val, err := d.decodeValue(path)
			if err != nil {
				return nil, err
			}
			entries[i] = map[string]any{"Key": key, "Value": val}
		}
		return entries, nil
	case binaryTagStruct:
		return d.decodeValueStruct(path)
	default:
		return nil, fmt.Errorf("invalid tag 0x%02x", tag)
	}
}

// decodeValueStruct decodes a TAG_STRUCT payload into map[string]any for
// the generic Decode path. For schema-typed structs it uses field index
// to look up field names from the View schema if available.
func (d *binaryDecoder) decodeValueStruct(path string) (any, error) {
	schemaID, err := d.readUvarint()
	if err != nil {
		return nil, fmt.Errorf("%s: read schema id: %w", path, err)
	}
	count, err := d.readUvarint()
	if err != nil {
		return nil, fmt.Errorf("%s: read field count: %w", path, err)
	}

	// No field name resolution in generic Decode path;
	// field names are synthesized as field_0, field_1, ...
	// callers that need named fields should use DecodeInto.

	m := make(map[string]any, count)
	if schemaID == 0 {
		// Anonymous struct: string-key encoding.
		for i := uint64(0); i < count; i++ {
			key, err := d.readString()
			if err != nil {
				return nil, fmt.Errorf("%s.field[%d]: %w", path, i, err)
			}
			val, err := d.decodeValue(path)
			if err != nil {
				return nil, err
			}
			m[key] = val
		}
	} else {
		// Schema-typed: field-index encoding.
		for i := uint64(0); i < count; i++ {
			fieldIdx, err := d.readUvarint()
			if err != nil {
				return nil, fmt.Errorf("%s.field[%d]: %w", path, i, err)
			}
			val, err := d.decodeValue(path)
			if err != nil {
				return nil, err
			}
			name := fmt.Sprintf("field_%d", fieldIdx)
			m[name] = val
		}
	}
	return m, nil
}

func binaryProjectValue(s schema.TypeDesc, value any) (any, error) {
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, fmt.Errorf("cannot encode nil pointer")
		}
		v = v.Elem()
	}

	if s.Kind == schema.TypeKindMap {
		if v.IsValid() && v.Kind() == reflect.Struct && isOrderedMapValue(v) {
			return orderedMapEntries(v)
		}
		// A map-kind scope projects either as a native Go map or as an ordered
		// entry sequence (a slice of {Key, Value} entries). The container is
		// chosen by the schema kind plus the declared wire version — not by a
		// value-shape heuristic — so any slice under a map scope is treated as
		// an entry sequence and validated as such.
		if v.IsValid() && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) && wireVersionSupportsEntrySequence() {
			entries := make([]any, v.Len())
			for i := 0; i < v.Len(); i++ {
				key, val, ok := entryFromProjectedValue(v.Index(i).Interface())
				if !ok {
					return nil, fmt.Errorf("invalid entry sequence element at index %d", i)
				}
				entries[i] = map[string]any{"Key": key, "Value": val}
			}
			return entries, nil
		}
	}

	return value, nil
}

func canonicalizeDecodedValue(view View, value any) (any, error) {
	switch view.Schema.Kind {
	case schema.TypeKindScalar:
		return canonicalizeScalar(view, value)
	case schema.TypeKindStruct:
		m, ok := value.(map[string]any)
		if !ok {
			return nil, decodeErrorWithTypes(view, "", "struct map", fmt.Sprintf("%T", value), "binary decode: expected struct map, got %T", value)
		}
		return m, nil
	case schema.TypeKindArray:
		arr, ok := value.([]any)
		if !ok {
			return nil, decodeErrorWithTypes(view, "", "array", fmt.Sprintf("%T", value), "binary decode: expected array, got %T", value)
		}
		return arr, nil
	case schema.TypeKindMap:
		switch value.(type) {
		case map[string]any, []any:
			return value, nil
		default:
			return nil, decodeErrorWithTypes(view, "", "map or entry sequence", fmt.Sprintf("%T", value), "binary decode: expected map or entry sequence, got %T", value)
		}
	case schema.TypeKindMedia:
		m, ok := value.(map[string]any)
		if !ok {
			return nil, decodeErrorWithTypes(view, "", "media map", fmt.Sprintf("%T", value), "binary decode: expected media map, got %T", value)
		}
		if err := schema.ValidateMediaValue(m); err != nil {
			return nil, decodeErrorWithTypes(view, "", "media", "invalid", "binary decode: %v", err)
		}
		return m, nil
	default:
		return value, nil
	}
}

func canonicalizeScalar(view View, value any) (any, error) {
	switch view.Schema.Name {
	case "int":
		switch v := value.(type) {
		case int32:
			return v, nil
		case int64:
			return int32(v), nil
		case uint64:
			return int32(v), nil
		default:
			return nil, decodeErrorWithTypes(view, "", "int-compatible scalar", fmt.Sprintf("%T", value), "binary decode: expected int-compatible scalar, got %T", value)
		}
	case "string":
		v, ok := value.(string)
		if !ok {
			return nil, decodeErrorWithTypes(view, "", "string", fmt.Sprintf("%T", value), "binary decode: expected string, got %T", value)
		}
		return v, nil
	case "bool":
		v, ok := value.(bool)
		if !ok {
			return nil, decodeErrorWithTypes(view, "", "bool", fmt.Sprintf("%T", value), "binary decode: expected bool, got %T", value)
		}
		return v, nil
	case "float", "double":
		switch v := value.(type) {
		case float64:
			return v, nil
		case int32:
			return float64(v), nil
		case int64:
			return float64(v), nil
		case uint64:
			return float64(v), nil
		default:
			return nil, decodeErrorWithTypes(view, "", "float-compatible scalar", fmt.Sprintf("%T", value), "binary decode: expected float-compatible scalar, got %T", value)
		}
	case "bytes":
		v, ok := value.([]byte)
		if !ok {
			return nil, decodeErrorWithTypes(view, "", "bytes", fmt.Sprintf("%T", value), "binary decode: expected bytes, got %T", value)
		}
		return v, nil
	default:
		return value, nil
	}
}

func entryFromProjectedValue(value any) (key any, val any, ok bool) {
	switch entry := value.(type) {
	case map[string]any:
		key, keyOK := entry["Key"]
		val, valOK := entry["Value"]
		return key, val, keyOK && valOK
	default:
		v := reflect.ValueOf(value)
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil, nil, false
			}
			v = v.Elem()
		}
		if !v.IsValid() || v.Kind() != reflect.Struct {
			return nil, nil, false
		}
		keyField := v.FieldByName("Key")
		valField := v.FieldByName("Value")
		if !keyField.IsValid() || !valField.IsValid() || !keyField.CanInterface() || !valField.CanInterface() {
			return nil, nil, false
		}
		return keyField.Interface(), valField.Interface(), true
	}
}

// jsonFieldName returns the wire name for a struct field. If the field has a
// `json:"name"` tag, the name portion is used; otherwise the Go field name.
func jsonFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag != "" {
		if idx := strings.Index(tag, ","); idx >= 0 {
			tag = tag[:idx]
		}
		if tag != "" && tag != "-" {
			return tag
		}
	}
	return f.Name
}
