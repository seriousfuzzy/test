package blockchain

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"

	badger "github.com/dgraph-io/badger"
)

var (
	utxoPrefix  = []byte("utxo-")
	prefiLength = len(utxoPrefix)
)

type UXTOSet struct {
	Blockchain *Blockchain
}

func (u *UXTOSet) FindSpendableOutputs(pubKeyHash []byte, amount int) (int, map[string][]int) {
	unspentOuts := make(map[string][]int)
	accumulated := 0

	db := u.Blockchain.Database

	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(utxoPrefix); it.ValidForPrefix(utxoPrefix); it.Next() {
			item := it.Item()
			k := item.KeyCopy(nil)

			v, err := item.ValueCopy(nil)
			Handle(err)
			outs := DeSerializeOutputs(v)

			k = bytes.TrimPrefix(k, utxoPrefix)
			txID := hex.EncodeToString(k)
			fmt.Println(txID)

			for outIdx, out := range outs.Outputs {
				if out.IsLockWithKey(pubKeyHash) && accumulated < amount {
					accumulated += out.Value
					unspentOuts[txID] = append(unspentOuts[txID], outIdx)
					if accumulated >= amount {
						break
					}
				}
			}
		}

		return nil
	})
	Handle(err)
	return accumulated, unspentOuts
}

func (u UXTOSet) FindUnSpentTransactions(pubKeyHash []byte) []TxOutput {
	var UTXOs []TxOutput
	db := u.Blockchain.Database

	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(utxoPrefix); it.ValidForPrefix(utxoPrefix); it.Next() {
			item := it.Item()
			v, err := item.ValueCopy(nil)

			Handle(err)
			outs := DeSerializeOutputs(v)

			for _, out := range outs.Outputs {
				if out.IsLockWithKey(pubKeyHash) {
					UTXOs = append(UTXOs, out)
				}
			}
		}

		return nil
	})
	Handle(err)

	return UTXOs
}

func (u *UXTOSet) CountTransactions() int {
	db := u.Blockchain.Database
	counter := 0
	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(utxoPrefix); it.ValidForPrefix(utxoPrefix); it.Next() {
			counter++
		}
		return nil
	})
	Handle(err)
	return counter
}
func (u *UXTOSet) Update(block *Block) {
	db := u.Blockchain.Database
	err := db.Update(func(txn *badger.Txn) error {
		for _, tx := range block.Transactions {
			if tx.IsCoinBase() == false {
				for _, in := range tx.Inputs {
					updatedOutputs := TxOutputs{}
					inID := append(utxoPrefix, in.ID...)
					item, err := txn.Get(inID)
					Handle(err)
					v, err := item.ValueCopy(nil)
					Handle(err)

					outs := DeSerializeOutputs(v)
					for outIdx, out := range outs.Outputs {
						if outIdx != in.Out {
							updatedOutputs.Outputs = append(updatedOutputs.Outputs, out)
						}
					}
					if len(updatedOutputs.Outputs) == 0 {
						if err := txn.Delete(inID); err != nil {
							log.Panic(err)
						}
					} else {
						if err := txn.Set(inID, updatedOutputs.Serialize()); err != nil {
							log.Panic(err)
						}
					}
				}
				newOutputs := TxOutputs{}
				for _, out := range tx.Outputs {
					newOutputs.Outputs = append(newOutputs.Outputs, out)
				}
				txID := append(utxoPrefix, tx.ID...)
				err := txn.Set(txID, newOutputs.Serialize())
				Handle(err)
			} else {

				//Update UXTO for coinbase(Miner Benefits) transactions
				newOutputs := TxOutputs{}
				for _, out := range tx.Outputs {
					newOutputs.Outputs = append(newOutputs.Outputs, out)
				}
				txID := append(utxoPrefix, tx.ID...)
				err := txn.Set(txID, newOutputs.Serialize())
				Handle(err)
			}
		}
		return nil
	})

	Handle(err)
}
func (u *UXTOSet) ReIndex() {
	db := u.Blockchain.Database

	u.DeleteByPrefix(utxoPrefix)

	UTXO := u.Blockchain.FindUTXO()

	err := db.Update(func(txn *badger.Txn) error {
		for txId, outs := range UTXO {
			key, err := hex.DecodeString(txId)
			Handle(err)

			key = append(utxoPrefix, key...)
			err = txn.Set(key, outs.Serialize())
			Handle(err)
		}
		return nil
	})

	Handle(err)
}
func (u *UXTOSet) DeleteByPrefix(prefix []byte) {
	deleteKeys := func(keysForDelete [][]byte) error {
		if err := u.Blockchain.Database.Update(func(txn *badger.Txn) error {
			for _, key := range keysForDelete {
				if err := txn.Delete(key); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		return nil
	}

	collectSize := 100000
	u.Blockchain.Database.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		keysForDelete := make([][]byte, 0, collectSize)
		keysCollected := 0
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := it.Item().KeyCopy(nil)
			keysForDelete = append(keysForDelete, key)
			keysCollected++
			if keysCollected == collectSize {
				if err := deleteKeys(keysForDelete); err != nil {
					log.Panic(err)
				}

				keysForDelete = make([][]byte, 0, collectSize)
				keysCollected = 0
			}
		}

		if keysCollected > 0 {
			if err := deleteKeys(keysForDelete); err != nil {
				log.Panic(err)
			}
		}

		return nil
	})

}
