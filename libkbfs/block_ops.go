package libkbfs

import (
	"github.com/keybase/client/go/libkb"
	"golang.org/x/net/context"
)

// BlockOpsStandard implements the BlockOps interface by relaying
// requests to the block server.
type BlockOpsStandard struct {
	config Config
}

var _ BlockOps = (*BlockOpsStandard)(nil)

// Get implements the BlockOps interface for BlockOpsStandard.
func (b *BlockOpsStandard) Get(ctx context.Context, md *RootMetadata,
	blockPtr BlockPointer, block Block) error {
	bserv := b.config.BlockServer()
	buf, blockServerHalf, err := bserv.Get(ctx, blockPtr.ID, blockPtr)
	if err != nil {
		return err
	}

	tlfCryptKey, err := b.config.KeyManager().
		GetTLFCryptKeyForBlockDecryption(ctx, md, blockPtr)
	if err != nil {
		return err
	}

	// construct the block crypt key
	blockCryptKey, err := b.config.Crypto().UnmaskBlockCryptKey(
		blockServerHalf, tlfCryptKey)
	if err != nil {
		return err
	}

	var encryptedBlock EncryptedBlock
	err = b.config.Codec().Decode(buf, &encryptedBlock)
	if err != nil {
		return err
	}

	// decrypt the block
	return b.config.Crypto().DecryptBlock(encryptedBlock, blockCryptKey, block)
}

// Ready implements the BlockOps interface for BlockOpsStandard.
func (b *BlockOpsStandard) Ready(ctx context.Context, md *RootMetadata,
	block Block) (id BlockID, plainSize int, readyBlockData ReadyBlockData,
	err error) {
	defer func() {
		if err != nil {
			id = BlockID{}
			plainSize = 0
			readyBlockData = ReadyBlockData{}
		}
	}()

	crypto := b.config.Crypto()

	tlfCryptKey, err := b.config.KeyManager().
		GetTLFCryptKeyForEncryption(ctx, md)
	if err != nil {
		return
	}

	// New server key half for the block.
	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	if err != nil {
		return
	}

	blockKey, err := crypto.UnmaskBlockCryptKey(serverHalf, tlfCryptKey)
	if err != nil {
		return
	}

	plainSize, encryptedBlock, err := crypto.EncryptBlock(block, blockKey)
	if err != nil {
		return
	}

	buf, err := b.config.Codec().Encode(encryptedBlock)
	if err != nil {
		return
	}

	readyBlockData = ReadyBlockData{
		buf:        buf,
		serverHalf: serverHalf,
	}

	encodedSize := readyBlockData.GetEncodedSize()
	if encodedSize < plainSize {
		err = TooLowByteCountError{
			ExpectedMinByteCount: plainSize,
			ByteCount:            encodedSize,
		}
		return
	}

	// now get the block ID for the buffer
	h, err := crypto.Hash(buf)
	if err != nil {
		return
	}

	nhs, ok := h.(libkb.NodeHashShort)
	if !ok {
		err = BadCryptoError{id}
		return
	}

	id = BlockID(nhs)

	return
}

// Put implements the BlockOps interface for BlockOpsStandard.
func (b *BlockOpsStandard) Put(ctx context.Context, md *RootMetadata,
	blockPtr BlockPointer, readyBlockData ReadyBlockData) error {
	bserv := b.config.BlockServer()
	return bserv.Put(ctx, blockPtr.ID, md.ID, blockPtr, readyBlockData.buf,
		readyBlockData.serverHalf)
}

// Delete implements the BlockOps interface for BlockOpsStandard.
func (b *BlockOpsStandard) Delete(ctx context.Context, md *RootMetadata,
	id BlockID, context BlockContext) error {
	bserv := b.config.BlockServer()
	err := bserv.DecBlockReference(ctx, id, md.ID, context)
	return err
}
