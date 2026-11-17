package savelegacy

import (
	"fmt"
	"reflect"

	"rimworld-go/internal/domain/pawn/skill"
)

// Upgrade maps old gob-compatible fields into their current representations.
// It runs solely when loading saves older than v18. Each dst is a pointer to
// the canonical field immediately followed by its legacy source value.
func Upgrade(fields ...any) error {
	if len(fields)%2 != 0 {
		return fmt.Errorf("legacy upgrade expects field pairs")
	}
	for i := 0; i < len(fields); i += 2 {
		dst := reflect.ValueOf(fields[i])
		if dst.Kind() != reflect.Pointer || dst.IsNil() {
			return fmt.Errorf("legacy field %d requires a destination pointer", i/2)
		}
		src := reflect.ValueOf(fields[i+1])
		if !src.IsValid() {
			continue
		}
		switch src.Kind() {
		case reflect.Map, reflect.Pointer, reflect.Slice, reflect.Interface:
			if src.IsNil() {
				continue
			}
		}
		if err := copyField(dst.Elem(), src); err != nil {
			return fmt.Errorf("legacy field %d: %w", i/2, err)
		}
	}
	return nil
}

// UpgradeAndClear upgrades destination/source pointer pairs and clears the legacy
// fields only after every conversion succeeds. marker identifies the canonical schema.
func UpgradeAndClear(marker *bool, fields ...any) error {
	if marker == nil {
		return fmt.Errorf("legacy upgrade requires a schema marker")
	}
	if len(fields)%2 != 0 {
		return fmt.Errorf("legacy upgrade expects destination/source pointer pairs")
	}
	pairs := make([]any, 0, len(fields))
	sources := make([]reflect.Value, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		source := reflect.ValueOf(fields[i+1])
		if source.Kind() != reflect.Pointer || source.IsNil() || !source.Elem().CanInterface() || !source.Elem().CanSet() {
			return fmt.Errorf("legacy field %d requires a settable source pointer", i/2)
		}
		pairs = append(pairs, fields[i], source.Elem().Interface())
		sources = append(sources, source.Elem())
	}
	if err := Upgrade(pairs...); err != nil {
		return err
	}
	for _, source := range sources {
		source.SetZero()
	}
	*marker = true
	return nil
}

func copyField(dst, src reflect.Value) error {
	if src.Type().AssignableTo(dst.Type()) {
		dst.Set(src)
		return nil
	}
	if src.Kind() != dst.Kind() {
		return fmt.Errorf("cannot convert %s to %s", src.Type(), dst.Type())
	}
	switch dst.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return nil
		}
		dst.Set(reflect.New(dst.Type().Elem()))
		return copyField(dst.Elem(), src.Elem())
	case reflect.Map:
		if src.IsNil() {
			return nil
		}
		if src.Type().Key() != dst.Type().Key() {
			return fmt.Errorf("different map keys: %s, %s", src.Type(), dst.Type())
		}
		dst.Set(reflect.MakeMapWithSize(dst.Type(), src.Len()))
		it := src.MapRange()
		for it.Next() {
			val := reflect.New(dst.Type().Elem()).Elem()
			if err := copyField(val, it.Value()); err != nil {
				return err
			}
			dst.SetMapIndex(it.Key(), val)
		}
	case reflect.Slice:
		if src.IsNil() {
			return nil
		}
		dst.Set(reflect.MakeSlice(dst.Type(), src.Len(), src.Len()))
		for i := 0; i < src.Len(); i++ {
			if err := copyField(dst.Index(i), src.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Array:
		if src.Len() > dst.Len() {
			return fmt.Errorf("array exceeds new width: %s -> %s", src.Type(), dst.Type())
		}
		for i := 0; i < src.Len(); i++ {
			if err := copyField(dst.Index(i), src.Index(i)); err != nil {
				return err
			}
		}
		if dst.Type() == reflect.TypeFor[skill.Set]() && src.Len() == int(skill.Artistic) {
			dst.Index(int(skill.Artistic)).Set(reflect.ValueOf(skill.Skill{Level: skill.Neutral}))
		}
	case reflect.Struct:
		for i := 0; i < dst.NumField(); i++ {
			field := dst.Type().Field(i)
			if !field.IsExported() {
				return fmt.Errorf("non-exported destination field %s", field.Name)
			}
			original := src.FieldByName(field.Name)
			if !original.IsValid() {
				continue // an additive current field has its zero value
			}
			if err := copyField(dst.Field(i), original); err != nil {
				return fmt.Errorf("%s: %w", field.Name, err)
			}
		}
	default:
		if src.Type().ConvertibleTo(dst.Type()) {
			dst.Set(src.Convert(dst.Type()))
			return nil
		}
		return fmt.Errorf("unsupported conversion %s -> %s", src.Type(), dst.Type())
	}
	return nil
}
