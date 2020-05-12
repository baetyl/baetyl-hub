package store

import (
	"bytes"
	"errors"
	"reflect"
	"time"

	bolt "go.etcd.io/bbolt"
)

func init() {
	Factories["boltdb"] = newBoltDB
}

// boltDB the backend BoltDB to persist values
type boltDB struct {
	*bolt.DB
	conf Conf
}

// boltBucket the bucket to save data
type boltBucket struct {
	db      *bolt.DB
	name    []byte
	encoder Encoder
}

// New creates a new boltDB database
func newBoltDB(conf Conf) (DB, error) {
	db, err := bolt.Open(conf.Source, 0600, &bolt.Options{Timeout: 30 * time.Second})
	if err != nil {
		return nil, err
	}
	return &boltDB{
		DB:   db,
		conf: conf,
	}, nil
}

// NewBucket creates a bucket
func (d *boltDB) NewBucket(name string, _encoder Encoder) (Bucket, error) {
	if _encoder == nil {
		_encoder = NewDefaultEncoder()
	}
	if err := d.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(name))
		return err
	}); err != nil {
		return nil, err
	}
	return &boltBucket{
		db:      d.DB,
		name:    []byte(name),
		encoder: _encoder,
	}, nil
}

// Put puts values into DB
func (d *boltBucket) Put(values []interface{}) error {
	if len(values) == 0 {
		return nil
	}
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(d.name)
		for i := range values {
			index, err := b.NextSequence()
			if err != nil {
				return err
			}
			gk := U64ToByte(index)
			gv, err := d.encoder.Encode(values[i])
			if err != nil {
				return err
			}
			err = b.Put(gk, gv)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Get gets values from DB
func (d *boltBucket) Get(offset uint64, length int, results interface{}) error {
	return d.db.View(func(tx *bolt.Tx) error {
		resultVal := reflect.ValueOf(results)
		if resultVal.Kind() != reflect.Ptr || resultVal.Elem().Kind() != reflect.Slice {
			return errors.New("result argument must be a slice address")
		}

		b := tx.Bucket(d.name)
		if b == nil {
			return errors.New("bucket doesn't exist")
		}

		sliceVal := resultVal.Elem()
		elType := sliceVal.Type().Elem()
		tp := elType
		for tp.Kind() == reflect.Ptr {
			tp = tp.Elem()
		}

		gk, gmax, c := U64ToByte(offset), U64ToByte(offset+uint64(length)), b.Cursor()
		for k, v := c.Seek(gk); k != nil && bytes.Compare(k, gmax) < 0; k, v = c.Next() {
			val := reflect.New(tp)
			err := d.encoder.Decode(v, val.Interface(), ByteToU64(k))
			if err != nil {
				return err
			}

			var rowValue reflect.Value
			if elType.Kind() == reflect.Ptr {
				rowValue = val
			} else {
				rowValue = val.Elem()
			}
			sliceVal = reflect.Append(sliceVal, rowValue)
		}
		resultVal.Elem().Set(sliceVal.Slice(0, sliceVal.Len()))
		return nil
	})
}

// Del deletes values by IDs from DB
func (d *boltBucket) Del(ids []uint64) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(d.name)
		if b == nil {
			return errors.New("bucket doesn't exist")
		}
		for _, v := range ids {
			gk := U64ToByte(v)
			if err := b.Delete(gk); err != nil {
				return err
			}
		}
		return nil
	})
}

// DelBefore delete expired messages from DB
func (d *boltBucket) DelBefore(ts time.Time) error {
	// TODO: cause boltDB not support ttl
	return nil
}

// * kv

// SetKV sets key and value into DB
func (d *boltBucket) SetKV(k string, v interface{}) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(d.name)
		if b == nil {
			return errors.New("bucket doesn't exist")
		}
		gv, err := d.encoder.Encode(v)
		if err != nil {
			return err
		}
		return b.Put([]byte(k), gv)
	})
}

// GetKV gets value by key from DB
func (d *boltBucket) GetKV(k string, result interface{}) error {
	return d.db.View(func(tx *bolt.Tx) error {
		resultVal := reflect.ValueOf(result)
		if resultVal.Kind() != reflect.Ptr {
			return errors.New("result argument must be a pointer")
		}

		b := tx.Bucket(d.name)
		if b == nil {
			return errors.New("bucket doesn't exist")
		}

		value := b.Get([]byte(k))
		if len(value) == 0 {
			return errors.New("No data found for this key")
		}
		return d.encoder.Decode(value, result)
	})
}

// Del deletes key and value from DB
func (d *boltBucket) DelKV(k string) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(d.name)
		if b == nil {
			return errors.New("bucket doesn't exist")
		}
		return b.Delete([]byte(k))
	})
}

// ListKV list key and value from DB
func (d *boltBucket) ListKV(results interface{}) error {
	return d.db.View(func(tx *bolt.Tx) error {
		resultVal := reflect.ValueOf(results)
		if resultVal.Kind() != reflect.Ptr || resultVal.Elem().Kind() != reflect.Slice {
			return errors.New("result argument must be a slice address")
		}

		b := tx.Bucket(d.name)
		if b == nil {
			return errors.New("bucket doesn't exist")
		}

		sliceVal := resultVal.Elem()
		elType := sliceVal.Type().Elem()
		tp := elType
		for tp.Kind() == reflect.Ptr {
			tp = tp.Elem()
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			val := reflect.New(tp)
			err := d.encoder.Decode(v, val.Interface())
			if err != nil {
				return err
			}

			var rowValue reflect.Value
			if elType.Kind() == reflect.Ptr {
				rowValue = val
			} else {
				rowValue = val.Elem()
			}
			sliceVal = reflect.Append(sliceVal, rowValue)
		}
		resultVal.Elem().Set(sliceVal.Slice(0, sliceVal.Len()))
		return nil
	})
}

// Close close bucket
func (d *boltBucket) Close(clean bool) (err error) {
	if clean {
		return d.db.Update(func(tx *bolt.Tx) error {
			return tx.DeleteBucket(d.name)
		})
	}
	return nil
}