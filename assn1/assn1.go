package assn1

import (
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/fenilfadadu/cs628-assn1/userlib"
)

type Privatekey = userlib.PrivateKey

var BlockSize = userlib.BlockSize

type User_r struct {
	KeyAddr   string
	Signature []byte
	User
}

type User struct {
	Username string
	Password string
	Privkey  *Privatekey
}

type Inode_r struct {
	KeyAddr   string
	Signature []byte
	Inode
}

type Inode struct {
	Filename     string
	ShRecordAddr string
	SymmKey      []byte
}

type SharingRecord_r struct {
	KeyAddr   string
	Signature []byte
	SharingRecord
}

type SharingRecord struct {
	Type       string
	MainAuthor string
	Address    []string
	SymmKey    [][]byte
}

type Data struct {
	KeyAddr   string
	Value     []byte
	Signature []byte
}

func remove(slice []byte, s int) []byte {
	return append(slice[:s], slice[s+1:]...)
}

//////////// DEBUG
func GetMapContent(key string) ([]byte, bool) {
	content, status := userlib.DatastoreGet(key)
	if !status {
		return []byte("Content not found"), status
	}
	return content, true
}

func SetMapContent(key string, value []byte) {
	userlib.DatastoreSet(key, value)
}

///////////// DEBUG

func GetUserKey(username string, password string) string {
	// Generate the key corresponding to provided user credentials
	passbyte := []byte(password + username)
	saltbyte := []byte(username + "user")

	// key = Argon2Key(password + username, username + "user", 10)
	keyHash := userlib.Argon2Key(passbyte, saltbyte, 10)
	marsh, err := json.Marshal(keyHash)
	if err != nil {
		userlib.DebugMsg("UserKey Marshalling failed")
	}

	// This is the key where encrypted User struct will be stored
	userKey := hex.EncodeToString(marsh)

	return userKey
}

func (user *User) GetInodeKey(filename string) string {
	// Generate the key corresponding to provided filename
	passbyte := []byte((*user).Password + filename)
	saltbyte := []byte((*user).Username + filename)

	// key = Argon2Key(password + filename, username + filename, 10)
	keyHash := userlib.Argon2Key(passbyte, saltbyte, 10)
	marsh, err := json.Marshal(keyHash)
	if err != nil {
		userlib.DebugMsg("FileKey Marshalling failed")
	}
	// This is the key where encrypted Inode struct for "filename" is stored
	fileKey := hex.EncodeToString(marsh)

	return fileKey
}

/////////////////

// This creates a user.  It will only be called once for a user
// (unless the keystore and datastore are cleared during testing purposes)

// It should store a copy of the userdata, suitably encrypted, in the
// datastore and should store the user's public key in the keystore.

// The datastore may corrupt or completely erase the stored
// information, but nobody outside should be able to get at the stored
// User data: the name used in the datastore should not be guessable
// without also knowing the password and username.

// You are not allowed to use any global storage other than the
// keystore and the datastore functions in the userlib library.

// You can assume the user has a STRONG password
func InitUser(username string, password string) (userdataptr *User, err error) {
	// Generate a Key for symmetric encryption and storage of User_r struct
	userKey := GetUserKey(username, password)
	userSymKey, err := hex.DecodeString(GetUserKey(password, username))
	if err != nil {
		return nil, errors.New(err.Error())
	}

	// Generate RSA Public-Private Key Pair for the User
	privKey, err := userlib.GenerateRSAKey()
	if err != nil {
		return nil, errors.New("RSA Key-Pair generation failed")
	}

	// Push the RSA Public Key to secure Key-Store
	userlib.KeystoreSet(username, privKey.PublicKey)

	// Initialize the User structure without any signature
	user := &User_r{
		KeyAddr: userKey, // The key at which this struct will be stored
		User: User{
			Username: username,
			Password: password,
			Privkey:  privKey,
		},
	}

	// Store the signature of User_r.User in User_r.Signature
	userMarsh, err := json.Marshal(user.User)
	if err != nil {
		return nil, errors.New("User_r.User Marshalling failed")
	}
	mac := userlib.NewHMAC(userSymKey)
	mac.Write(userMarsh)
	user.Signature = mac.Sum(nil)

	// Finally, encrypt the whole thing
	user_rMarsh, err := json.Marshal(user)
	if err != nil {
		return nil, errors.New("User_r Marshalling failed")
	}

	ciphertext := make([]byte, BlockSize+len(user_rMarsh))
	iv := ciphertext[:BlockSize]
	copy(iv, userlib.RandomBytes(BlockSize))

	// userlib.DebugMsg("Random IV", hex.EncodeToString(iv))
	// NOTE: The "key" needs to be of 16 bytes
	cipher := userlib.CFBEncrypter(userSymKey[:16], iv)
	cipher.XORKeyStream(ciphertext[BlockSize:], []byte(user_rMarsh))

	// Push the encrypted data to Untrusted Data Store
	userlib.DatastoreDelete(userKey)
	userlib.DatastoreSet(userKey, ciphertext)

	return &user.User, nil
}

// This fetches the user information from the Datastore.  It should
// fail with an error if the user/password is invalid, or if the user
// data was corrupted, or if the user can't be found.
func GetUser(username string, password string) (userdataptr *User, err error) {
	// Retrieve the Key for symmetric encryption and storage of User_r struct
	userKey := GetUserKey(username, password)
	userSymKey, err := hex.DecodeString(GetUserKey(password, username))
	if err != nil {
		return nil, errors.New(err.Error())
	}

	// Now, retrieve and decrypt the User_r struct and check if the
	// credentials and integrity are properly maintained.
	ciphertext, status := userlib.DatastoreGet(userKey)
	if status != true {
		return nil, errors.New("User not found")
	}

	iv := ciphertext[:BlockSize]
	cipher := userlib.CFBDecrypter(userSymKey[:16], iv)

	// In place AES decryption of ciphertext
	cipher.XORKeyStream(ciphertext[BlockSize:], ciphertext[BlockSize:])

	var user User_r
	err = json.Unmarshal(ciphertext[BlockSize:], &user)
	if err != nil {
		return nil, errors.New("User_r Unmarshalling failed")
	}

	// Verify the User_r struct's integrity
	userMarsh, err := json.Marshal(user.User)
	if err != nil {
		return nil, errors.New("User_r.User Marshalling failed")
	}

	mac := userlib.NewHMAC(userSymKey)
	mac.Write(userMarsh)
	if !userlib.Equal(user.Signature, mac.Sum(nil)) {
		return nil, errors.New("User Integrity check failed")
	}

	//
	// Cool, after verifying the integrity, cross check the credentials
	// just to be sure about user authentication
	if username != user.User.Username || password != user.User.Password {
		return nil, errors.New("Error: User credentials don't match")
	}

	if userKey != user.KeyAddr {
		return nil, errors.New("Error: Key-Value-Swap Attack")
	}

	// Everything works fine
	return &user.User, nil
}

// This stores a file in the datastore.
//
// The name of the file should NOT be revealed to the datastore!
func (user *User) StoreFile(filename string, data []byte) {
	///////////////////////////////////////
	//           INODE STRUCTURE         //
	///////////////////////////////////////
	fileKey := user.GetInodeKey(filename)

	// Check if the Inode for filename already exists
	rsaEncrypted, status := userlib.DatastoreGet(fileKey)
	if status {
		//
		// Since the Inode exists, we just need to overwrite the addess and
		// symmetric keys at SharingRecord structure, apart from actually
		// writing the data to DataStore
		var encrypted [][]byte
		err := json.Unmarshal(rsaEncrypted, &encrypted)
		if err != nil {
			return
		}

		// Retreive the Marshalled Inode_r struct from the encrypted chunks
		index := 0
		inodeMarsh := make([]byte, len(encrypted))
		for index < len(encrypted) {
			// RSA Asymmetric Key Decryption
			decryptedBlock, err := userlib.RSADecrypt(user.Privkey,
				encrypted[index], []byte("Tag"))
			if err != nil {
				return
			}

			inodeMarsh = append(inodeMarsh, decryptedBlock...)
			index += 1
		}

		// Remove leading \x00 characters from inodeMarsh
		for inodeMarsh[0] == []byte("\x00")[0] {
			inodeMarsh = remove(inodeMarsh, 0)
		}

		var file Inode_r
		err = json.Unmarshal(inodeMarsh, &file)
		if err != nil {
			return
		}

		// Verify Inode structure's integrity
		fileMarsh, err := json.Marshal(file.Inode)
		if err != nil {
			return
		}

		err = userlib.RSAVerify(&user.Privkey.PublicKey, fileMarsh, file.Signature)
		if err != nil {
			return
		}

		//
		///////////////////////////////////////
		//      SHARINGRECORD STRUCTURE      //
		///////////////////////////////////////
		// Retrieve encrypted SharingRecord structure from DataStore
		shrCipher, status := userlib.DatastoreGet(file.Inode.ShRecordAddr)
		if !status {
			return
		}

		iv := shrCipher[:BlockSize]
		cipher := userlib.CFBDecrypter(file.Inode.SymmKey, iv)

		// In place AES decryption of ciphertext
		cipher.XORKeyStream(shrCipher[BlockSize:], shrCipher[BlockSize:])

		var shrecord SharingRecord_r
		err = json.Unmarshal(shrCipher[BlockSize:], &shrecord)
		if err != nil {
			return
		}

		// Verify the integrity of SharingRecord structure
		shrMarsh, err := json.Marshal(shrecord.SharingRecord)
		if err != nil {
			return
		}
		mac := userlib.NewHMAC(file.Inode.SymmKey)
		mac.Write(shrMarsh)
		if !userlib.Equal(shrecord.Signature, mac.Sum(nil)) {
			return
		}

		// Appending a new block
		// Generate a random Initialization Vector and random address for
		// encryption of Data Block to be stored in SharingRecord structure
		randbyte, _ := json.Marshal(userlib.RandomBytes(BlockSize))
		randbyte, _ = json.Marshal(randbyte) // Double shuffling to reduce collision
		address := hex.EncodeToString(randbyte[:16])
		var addr []string
		var keys [][]byte

		shrecord.SharingRecord.Address = append(addr, address)
		shrecord.SharingRecord.SymmKey = append(keys, randbyte[:16])

		//
		// Now, Store the modified, encrypted and re-signed SharingRecord structure
		// back to the DataStore
		//

		// HMAC Signature via symmetric keys
		// Store the signature of SharingRecord_r.SharingRecord in Signature
		shrMarsh, err = json.Marshal(shrecord.SharingRecord)
		if err != nil {
			return
		}
		mac = userlib.NewHMAC(file.Inode.SymmKey)
		mac.Write(shrMarsh)
		shrecord.Signature = mac.Sum(nil)

		// Finally, encrypt the whole SharingRecord_r structure
		shrecord_rMarsh, err := json.Marshal(shrecord)
		if err != nil {
			return
		}

		ciphertext := make([]byte, BlockSize+len(shrecord_rMarsh))
		iv = ciphertext[:BlockSize]
		copy(iv, userlib.RandomBytes(BlockSize))

		// NOTE: The "key" needs to be of 16 bytes
		cipher = userlib.CFBEncrypter(file.Inode.SymmKey, iv) // Check [:16]
		cipher.XORKeyStream(ciphertext[BlockSize:], []byte(shrecord_rMarsh))

		//
		// Finally, push the data to be encrypted back to DataStore
		///////////////////////////////////////
		//           DATA STRUCTURE          //
		///////////////////////////////////////
		dbkey := randbyte[:16]

		// HMAC Signature of data block via symmetric key
		mac = userlib.NewHMAC(dbkey)
		mac.Write(data)
		hmacSum := mac.Sum(nil)

		dblock := &Data{
			// The key at which this struct will be stored
			KeyAddr:   address,
			Value:     data,
			Signature: hmacSum,
		}

		// Finally, encrypt the whole data block using Symmetric Key
		dblockMarsh, err := json.Marshal(dblock)
		if err != nil {
			return
		}

		cipherdata := make([]byte, BlockSize+len(dblockMarsh))
		iv = cipherdata[:BlockSize]
		copy(iv, userlib.RandomBytes(BlockSize))

		// NOTE: The "key" needs to be of 16 bytes
		cipher = userlib.CFBEncrypter(dbkey, iv)
		cipher.XORKeyStream(cipherdata[BlockSize:], []byte(dblockMarsh))

		//
		// Push the AES-CFB Encrypted SharingRecord structure to Data Store
		userlib.DatastoreDelete(file.Inode.ShRecordAddr)
		userlib.DatastoreSet(file.Inode.ShRecordAddr, ciphertext)

		// Push the AES-CFB Encrypted data block structure to Data Store
		userlib.DatastoreDelete(address)
		userlib.DatastoreSet(address, cipherdata)
	}

	//
	// Initialize the Inode structure without any signature (at the moment)
	//

	// Generate a random Initialization Vector and random address for
	// encryption of SharingRecord Structure
	iv := make([]byte, BlockSize)
	copy(iv, userlib.RandomBytes(BlockSize))

	randbyte, _ := json.Marshal(userlib.RandomBytes(BlockSize))
	randbyte, _ = json.Marshal(randbyte)
	address := hex.EncodeToString(randbyte[:16])

	file := &Inode_r{
		KeyAddr: fileKey, // The key at which this struct will be stored
		Inode: Inode{
			Filename:     filename,
			ShRecordAddr: address,
			SymmKey:      randbyte[:16],
		},
	}

	// Store the signature of Inode_r.Inode in Inode_r.Signature
	fileMarsh, err := json.Marshal(file.Inode)
	if err != nil {
		return
	}

	file.Signature, err = userlib.RSASign(user.Privkey, fileMarsh)
	if err != nil {
		return
	}

	// Finally, encrypt the whole Inode_r struct with User's Public key
	inodeMarsh, err := json.Marshal(file)
	if err != nil {
		return
	}

	// To store encrypted chunks
	var encrypted [][]byte
	var encryptedBlock []byte
	index := 0

	for index+190 <= len(inodeMarsh) {
		// RSA Asymmetric Key Encryption
		encryptedBlock, err = userlib.RSAEncrypt(&user.Privkey.PublicKey,
			inodeMarsh[index:index+190], []byte("Tag"))
		if err != nil {
			return
		}
		index += 190
		encrypted = append(encrypted, encryptedBlock)
	}

	// In case the final chunk is not a multiple of 190
	encryptedBlock, err = userlib.RSAEncrypt(&user.Privkey.PublicKey,
		inodeMarsh[index:], []byte("Tag"))
	if err != nil {
		return
	}
	encrypted = append(encrypted, encryptedBlock)

	encryptedMarsh, err := json.Marshal(encrypted)
	if err != nil {
		return
	}

	//
	///////////////////////////////////////
	//      SHARINGRECORD STRUCTURE      //
	///////////////////////////////////////

	var addr []string
	var keys [][]byte

	// Generate a random Initialization Vector and random address for
	// encryption of Data Block to be stored in SharingRecord structure
	randbyte, _ = json.Marshal(userlib.RandomBytes(BlockSize))
	randbyte, _ = json.Marshal(randbyte)
	address = hex.EncodeToString(randbyte[:16])

	// Here, we append the first block of data to the list of blocks
	// The address and the encryption key for the block
	addr = append(addr, address)
	keys = append(keys, randbyte[:16])

	shrecord := &SharingRecord_r{
		KeyAddr: file.Inode.ShRecordAddr, // The key at which this struct will be stored
		SharingRecord: SharingRecord{
			Type:       "Sharing Record",
			MainAuthor: user.Username,
			Address:    addr,
			SymmKey:    keys,
		},
	}

	// HMAC Signature via symmetric keys
	// Store the signature of SharingRecord_r.SharingRecord in Signature
	shrMarsh, err := json.Marshal(shrecord.SharingRecord)
	if err != nil {
		return
	}
	mac := userlib.NewHMAC(file.Inode.SymmKey)
	mac.Write(shrMarsh)
	shrecord.Signature = mac.Sum(nil)

	// Finally, encrypt the whole SharingRecord_r structure
	shrecord_rMarsh, err := json.Marshal(shrecord)
	if err != nil {
		return
	}

	ciphertext := make([]byte, BlockSize+len(shrecord_rMarsh))
	iv = ciphertext[:BlockSize]
	copy(iv, userlib.RandomBytes(BlockSize))

	// NOTE: The "key" needs to be of 16 bytes
	cipher := userlib.CFBEncrypter(file.Inode.SymmKey, iv) // Check [:16]
	cipher.XORKeyStream(ciphertext[BlockSize:], []byte(shrecord_rMarsh))

	//
	///////////////////////////////////////
	//           DATA STRUCTURE          //
	///////////////////////////////////////
	dbkey := shrecord.SharingRecord.SymmKey[0]

	// HMAC Signature of data block via symmetric key
	mac = userlib.NewHMAC(dbkey)
	mac.Write(data)
	hmacSum := mac.Sum(nil)

	dblock := &Data{
		// The key at which this struct will be stored
		KeyAddr:   shrecord.SharingRecord.Address[0],
		Value:     data,
		Signature: hmacSum,
	}

	// Finally, encrypt the whole data block using Symmetric Key
	dblockMarsh, err := json.Marshal(dblock)
	if err != nil {
		return
	}

	cipherdata := make([]byte, BlockSize+len(dblockMarsh))
	iv = cipherdata[:BlockSize]
	copy(iv, userlib.RandomBytes(BlockSize))

	// NOTE: The "key" needs to be of 16 bytes
	cipher = userlib.CFBEncrypter(dbkey, iv)
	cipher.XORKeyStream(cipherdata[BlockSize:], []byte(dblockMarsh))

	//
	// Push the RSA Encrypted Inode structure to Data Store
	userlib.DatastoreDelete(fileKey)
	userlib.DatastoreSet(fileKey, encryptedMarsh)

	// Push the AES-CFB Encrypted SharingRecord structure to Data Store
	userlib.DatastoreDelete(file.Inode.ShRecordAddr)
	userlib.DatastoreSet(file.Inode.ShRecordAddr, ciphertext)

	// Push the AES-CFB Encrypted data block structure to Data Store
	userlib.DatastoreDelete(shrecord.SharingRecord.Address[0])
	userlib.DatastoreSet(shrecord.SharingRecord.Address[0], cipherdata)

}

// This adds on to an existing file.
//
// Append should be efficient, you shouldn't rewrite or reencrypt the
// existing file, but only whatever additional information and
// metadata you need.
func (user *User) AppendFile(filename string, data []byte) (err error) {
	///////////////////////////////////////
	//           INODE STRUCTURE         //
	///////////////////////////////////////
	fileKey := user.GetInodeKey(filename)

	// Retrieve the encrypted Inode structure from DataStore
	rsaEncrypted, status := userlib.DatastoreGet(fileKey)
	if status != true {
		return errors.New("Filename not found")
	}

	var encrypted [][]byte
	err = json.Unmarshal(rsaEncrypted, &encrypted)
	if err != nil {
		return errors.New("Inode_r Unmarshalling failed")
	}

	// Retreive the Marshalled Inode_r struct from the encrypted chunks
	index := 0
	inodeMarsh := make([]byte, len(encrypted))
	for index < len(encrypted) {
		// RSA Asymmetric Key Decryption
		decryptedBlock, err := userlib.RSADecrypt(user.Privkey,
			encrypted[index], []byte("Tag"))
		if err != nil {
			return errors.New("RSA Encryption of Inode_r failed\n")
		}

		inodeMarsh = append(inodeMarsh, decryptedBlock...)
		index += 1
	}

	// Remove leading \x00 characters from inodeMarsh
	for inodeMarsh[0] == []byte("\x00")[0] {
		inodeMarsh = remove(inodeMarsh, 0)
	}

	var file Inode_r
	err = json.Unmarshal(inodeMarsh, &file)
	if err != nil {
		return errors.New("Inode_r Unmarshalling failed")
	}

	// Verify Inode structure's integrity
	fileMarsh, err := json.Marshal(file.Inode)
	if err != nil {
		return errors.New("Inode_r.Inode Marshalling failed")
	}

	err = userlib.RSAVerify(&user.Privkey.PublicKey, fileMarsh, file.Signature)
	if err != nil {
		return errors.New("Inode Integrity Check failed")
	}

	//
	///////////////////////////////////////
	//      SHARINGRECORD STRUCTURE      //
	///////////////////////////////////////
	// Retrieve encrypted SharingRecord structure from DataStore
	shrCipher, status := userlib.DatastoreGet(file.Inode.ShRecordAddr)
	if !status {
		return errors.New("Sharing Record Structure can't be found")
	}

	iv := shrCipher[:BlockSize]
	cipher := userlib.CFBDecrypter(file.Inode.SymmKey, iv)

	// In place AES decryption of ciphertext
	cipher.XORKeyStream(shrCipher[BlockSize:], shrCipher[BlockSize:])

	var shrecord SharingRecord_r
	err = json.Unmarshal(shrCipher[BlockSize:], &shrecord)
	if err != nil {
		return errors.New("SharingRecord_r Unmarshalling failed")
	}

	// Verify the integrity of SharingRecord structure
	shrMarsh, err := json.Marshal(shrecord.SharingRecord)
	if err != nil {
		return errors.New("SharingRecord_r.SharingRecord Marshalling failed")
	}
	mac := userlib.NewHMAC(file.Inode.SymmKey)
	mac.Write(shrMarsh)
	if !userlib.Equal(shrecord.Signature, mac.Sum(nil)) {
		return errors.New("SharingRecord Integrity check failed")
	}

	// Appending a new block
	// Generate a random Initialization Vector and random address for
	// encryption of Data Block to be stored in SharingRecord structure
	randbyte, _ := json.Marshal(userlib.RandomBytes(BlockSize))
	randbyte, _ = json.Marshal(randbyte)
	address := hex.EncodeToString(randbyte[:16])

	shrecord.SharingRecord.Address = append(shrecord.SharingRecord.Address, address)
	shrecord.SharingRecord.SymmKey = append(shrecord.SharingRecord.SymmKey, randbyte[:16])

	//
	// Now, Store the modified, encrypted and re-signed SharingRecord structure
	// back to the DataStore
	//

	// HMAC Signature via symmetric keys
	// Store the signature of SharingRecord_r.SharingRecord in Signature
	shrMarsh, err = json.Marshal(shrecord.SharingRecord)
	if err != nil {
		return errors.New("SharingRecord_r.SharingRecord Marshalling failed")
	}
	mac = userlib.NewHMAC(file.Inode.SymmKey)
	mac.Write(shrMarsh)
	shrecord.Signature = mac.Sum(nil)

	// Finally, encrypt the whole SharingRecord_r structure
	shrecord_rMarsh, err := json.Marshal(shrecord)
	if err != nil {
		return errors.New("SharingRecord_r Marshalling failed")
	}

	ciphertext := make([]byte, BlockSize+len(shrecord_rMarsh))
	iv = ciphertext[:BlockSize]
	copy(iv, userlib.RandomBytes(BlockSize))

	// NOTE: The "key" needs to be of 16 bytes
	cipher = userlib.CFBEncrypter(file.Inode.SymmKey, iv) // Check [:16]
	cipher.XORKeyStream(ciphertext[BlockSize:], []byte(shrecord_rMarsh))

	//
	// Finally, push the data to be encrypted back to DataStore
	///////////////////////////////////////
	//           DATA STRUCTURE          //
	///////////////////////////////////////
	dbkey := randbyte[:16]

	// HMAC Signature of data block via symmetric key
	mac = userlib.NewHMAC(dbkey)
	mac.Write(data)
	hmacSum := mac.Sum(nil)

	dblock := &Data{
		// The key at which this struct will be stored
		KeyAddr:   address,
		Value:     data,
		Signature: hmacSum,
	}

	// Finally, encrypt the whole data block using Symmetric Key
	dblockMarsh, err := json.Marshal(dblock)
	if err != nil {
		return errors.New("Data block Marshalling failed")
	}

	cipherdata := make([]byte, BlockSize+len(dblockMarsh))
	iv = cipherdata[:BlockSize]
	copy(iv, userlib.RandomBytes(BlockSize))

	// NOTE: The "key" needs to be of 16 bytes
	cipher = userlib.CFBEncrypter(dbkey, iv) // Check [:16]
	cipher.XORKeyStream(cipherdata[BlockSize:], []byte(dblockMarsh))

	//
	// Push the AES-CFB Encrypted SharingRecord structure to Data Store
	userlib.DatastoreDelete(file.Inode.ShRecordAddr)
	userlib.DatastoreSet(file.Inode.ShRecordAddr, ciphertext)

	// Push the AES-CFB Encrypted data block structure to Data Store
	userlib.DatastoreDelete(address)
	userlib.DatastoreSet(address, cipherdata)

	return nil
}

// This loads a file from the Datastore.
//
// It should give an error if the file is corrupted in any way.
func (user *User) LoadFile(filename string) (data []byte, err error) {
	///////////////////////////////////////
	//           INODE STRUCTURE         //
	///////////////////////////////////////
	fileKey := user.GetInodeKey(filename)

	// Retrieve the encrypted Inode structure from DataStore
	rsaEncrypted, status := userlib.DatastoreGet(fileKey)
	if status != true {
		return nil, errors.New("Filename not found")
	}

	var encrypted [][]byte
	err = json.Unmarshal(rsaEncrypted, &encrypted)
	if err != nil {
		return nil, errors.New("Inode_r Unmarshalling failed")
	}

	// Retreive the Marshalled Inode_r struct from the encrypted chunks
	index := 0
	inodeMarsh := make([]byte, len(encrypted))
	for index < len(encrypted) {
		// RSA Asymmetric Key Decryption
		decryptedBlock, err := userlib.RSADecrypt(user.Privkey,
			encrypted[index], []byte("Tag"))
		if err != nil {
			return nil, errors.New("RSA Encryption of Inode_r failed\n")
		}

		inodeMarsh = append(inodeMarsh, decryptedBlock...)
		index += 1
	}

	// Remove leading \x00 characters from inodeMarsh
	for inodeMarsh[0] == []byte("\x00")[0] {
		inodeMarsh = remove(inodeMarsh, 0)
	}

	var file Inode_r
	err = json.Unmarshal(inodeMarsh, &file)
	if err != nil {
		return nil, errors.New("Inode_r Unmarshalling failed")
	}

	// Verify Inode structure's integrity
	fileMarsh, err := json.Marshal(file.Inode)
	if err != nil {
		return nil, errors.New("Inode_r.Inode Marshalling failed")
	}

	err = userlib.RSAVerify(&user.Privkey.PublicKey, fileMarsh, file.Signature)
	if err != nil {
		return nil, errors.New("Inode Integrity Check failed")
	}

	//
	///////////////////////////////////////
	//      SHARINGRECORD STRUCTURE      //
	///////////////////////////////////////
	// Retrieve encrypted SharingRecord structure from DataStore
	shrCipher, status := userlib.DatastoreGet(file.Inode.ShRecordAddr)
	if !status {
		return nil, errors.New("Sharing Record Structure can't be found")
	}

	iv := shrCipher[:BlockSize]
	cipher := userlib.CFBDecrypter(file.Inode.SymmKey, iv)

	// In place AES decryption of ciphertext
	cipher.XORKeyStream(shrCipher[BlockSize:], shrCipher[BlockSize:])

	var shrecord SharingRecord_r
	err = json.Unmarshal(shrCipher[BlockSize:], &shrecord)
	if err != nil {
		return nil, errors.New("SharingRecord_r Unmarshalling failed")
	}

	// Verify the integrity of SharingRecord structure
	shrMarsh, err := json.Marshal(shrecord.SharingRecord)
	if err != nil {
		return nil, errors.New("SharingRecord_r.SharingRecord Marshalling failed")
	}
	mac := userlib.NewHMAC(file.Inode.SymmKey)
	mac.Write(shrMarsh)
	if !userlib.Equal(shrecord.Signature, mac.Sum(nil)) {
		return nil, errors.New("SharingRecord Integrity check failed")
	}

	//
	// Loop through all data blocks, run the checks and return them
	///////////////////////////////////////
	//           DATA STRUCTURE          //
	///////////////////////////////////////
	numBlocks := len(shrecord.SharingRecord.SymmKey)
	var finalData []byte

	for i := 0; i < numBlocks; i += 1 {
		dbKey := shrecord.SharingRecord.Address[i]
		symmKey := shrecord.SharingRecord.SymmKey[i]

		ciphertext, status := userlib.DatastoreGet(dbKey)
		if status != true {
			return nil, errors.New("Data block not found")
		}

		iv := ciphertext[:BlockSize]
		cipher := userlib.CFBDecrypter(symmKey, iv)

		// In place AES decryption of ciphertext
		cipher.XORKeyStream(ciphertext[BlockSize:],
			ciphertext[BlockSize:])

		var data Data
		err = json.Unmarshal(ciphertext[BlockSize:], &data)
		if err != nil {
			return nil, errors.New("Data block Unmarshalling failed")
		}

		// Check the data integrity
		mac := userlib.NewHMAC(symmKey)
		mac.Write(data.Value)
		hmacSum := mac.Sum(nil)
		if !userlib.Equal(data.Signature, hmacSum) {
			return nil, errors.New("Data Integrity check failed")
		}

		// Key-value swap check
		if dbKey != data.KeyAddr {
			return nil, errors.New("Key Value swap detected")
		}

		finalData = append(finalData, data.Value...)
	}

	return finalData, nil

}

// This creates a sharing record, which is a key pointing to something
// in the datastore to share with the recipient.

// This enables the recipient to access the encrypted file as well
// for reading/appending.

// Note that neither the recipient NOR the datastore should gain any
// information about hat the sender calls the file.  Only the
// recipient can access the sharing record, and only the recipient
// should be able to know the sender.

func (user *User) ShareFile(filename string, recipient string) (
	msgid string, err error) {
	///////////////////////////////////////
	//           INODE STRUCTURE         //
	///////////////////////////////////////
	fileKey := user.GetInodeKey(filename)

	// Retrieve the encrypted Inode structure from DataStore
	rsaEncrypted, status := userlib.DatastoreGet(fileKey)
	if status != true {
		return "", errors.New("Filename not found")
	}

	var encrypted [][]byte
	err = json.Unmarshal(rsaEncrypted, &encrypted)
	if err != nil {
		return "", errors.New("Inode_r Unmarshalling failed")
	}

	// Retreive the Marshalled Inode_r struct from the encrypted chunks
	index := 0
	inodeMarsh := make([]byte, len(encrypted))
	for index < len(encrypted) {
		// RSA Asymmetric Key Decryption
		decryptedBlock, err := userlib.RSADecrypt(user.Privkey,
			encrypted[index], []byte("Tag"))
		if err != nil {
			return "", errors.New("RSA Encryption of Inode_r failed\n")
		}

		inodeMarsh = append(inodeMarsh, decryptedBlock...)
		index += 1
	}

	// Remove leading \x00 characters from inodeMarsh
	for inodeMarsh[0] == []byte("\x00")[0] {
		inodeMarsh = remove(inodeMarsh, 0)
	}

	var file Inode_r
	err = json.Unmarshal(inodeMarsh, &file)
	if err != nil {
		return "", errors.New("Inode_r Unmarshalling failed")
	}

	// Verify Inode structure's integrity
	fileMarsh, err := json.Marshal(file.Inode)
	if err != nil {
		return "", errors.New("Inode_r.Inode Marshalling failed")
	}

	err = userlib.RSAVerify(&user.Privkey.PublicKey, fileMarsh, file.Signature)
	if err != nil {
		return "", errors.New("Inode Integrity Check failed")
	}

	//
	///////////////////////////////////////
	//      SHARINGRECORD STRUCTURE      //
	///////////////////////////////////////
	// Retrieve encrypted SharingRecord structure from DataStore
	shrCipher, status := userlib.DatastoreGet(file.Inode.ShRecordAddr)
	if !status {
		return "", errors.New("Sharing Record Structure can't be found")
	}

	iv := shrCipher[:BlockSize]
	cipher := userlib.CFBDecrypter(file.Inode.SymmKey, iv)

	// In place AES decryption of ciphertext
	cipher.XORKeyStream(shrCipher[BlockSize:], shrCipher[BlockSize:])

	var shrecord SharingRecord_r
	err = json.Unmarshal(shrCipher[BlockSize:], &shrecord)
	if err != nil {
		return "", errors.New("SharingRecord_r Unmarshalling failed")
	}

	// Verify the integrity of SharingRecord structure
	shrMarsh, err := json.Marshal(shrecord.SharingRecord)
	if err != nil {
		return "", errors.New("SharingRecord_r.SharingRecord Marshalling failed")
	}
	mac := userlib.NewHMAC(file.Inode.SymmKey)
	mac.Write(shrMarsh)
	if !userlib.Equal(shrecord.Signature, mac.Sum(nil)) {
		return "", errors.New("SharingRecord Integrity check failed")
	}

	// Loop through all data blocks, run the checks on them
	///////////////////////////////////////
	//           DATA STRUCTURE          //
	///////////////////////////////////////
	numBlocks := len(shrecord.SharingRecord.SymmKey)
	for i := 0; i < numBlocks; i += 1 {
		dbKey := shrecord.SharingRecord.Address[i]
		symmKey := shrecord.SharingRecord.SymmKey[i]

		ciphertext, status := userlib.DatastoreGet(dbKey)
		if status != true {
			return "", errors.New("Data block not found")
		}

		iv := ciphertext[:BlockSize]
		cipher := userlib.CFBDecrypter(symmKey, iv)

		// In place AES decryption of ciphertext
		cipher.XORKeyStream(ciphertext[BlockSize:],
			ciphertext[BlockSize:])

		var data Data
		err = json.Unmarshal(ciphertext[BlockSize:], &data)
		if err != nil {
			return "", errors.New("Data block Unmarshalling failed")
		}

		// Check the data integrity
		mac := userlib.NewHMAC(symmKey)
		mac.Write(data.Value)
		hmacSum := mac.Sum(nil)
		if !userlib.Equal(data.Signature, hmacSum) {
			return "", errors.New("Data Integrity check failed")
		}

		// Key-value swap check
		if dbKey != data.KeyAddr {
			return "", errors.New("Key Value swap detected")
		}
	}

	//
	// Find collected_info and return after appropriate verification
	collected_info := struct {
		SymmKey      []byte
		ShRecordAddr string
	}{
		file.Inode.SymmKey,
		file.Inode.ShRecordAddr,
	}

	recvPubKey, status := userlib.KeystoreGet(recipient)
	if !status {
		return "", errors.New("Recipient not found")
	}

	// Store the signature and encoding of collected_info
	infoMarsh, err := json.Marshal(collected_info)
	if err != nil {
		return "", errors.New("Collected Info Marshalling failed")
	}

	infoSign, err := userlib.RSASign(user.Privkey, infoMarsh)
	if err != nil {
		return "", errors.New("RSA Signing of Collected_info failed")
	}

	// Finally, encrypt the whole Packet struct with reciever's Public key
	send_info := struct {
		Collected_info []byte
		Signature      []byte
	}{
		infoMarsh,
		infoSign,
	}

	mgsidMarsh, err := json.Marshal(send_info)
	if err != nil {
		return "", errors.New("msgid Marshalling failed")
	}

	// To store encrypted chunks
	var sharing [][]byte
	var encryptedBlock []byte
	index = 0

	for index+190 <= len(mgsidMarsh) {
		// RSA Asymmetric Key Encryption
		encryptedBlock, err = userlib.RSAEncrypt(&recvPubKey,
			mgsidMarsh[index:index+190], []byte("Tag"))
		if err != nil {
			return "", errors.New("RSA Encryption of 'sharing' failed\n")
		}
		index += 190
		sharing = append(sharing, encryptedBlock)
	}

	// In case the final chunk is not a multiple of 190
	encryptedBlock, err = userlib.RSAEncrypt(&recvPubKey,
		mgsidMarsh[index:], []byte("Tag"))
	if err != nil {
		return "", errors.New("RSA Encryption of 'sharing' failed\n")
	}
	sharing = append(sharing, encryptedBlock)

	sharingMarsh, err := json.Marshal(sharing)
	if err != nil {
		return "", errors.New("Marshalling of sharing structure failed")
	}

	return hex.EncodeToString(sharingMarsh), nil

}

// Note recipient's filename can be different from the sender's filename.
// The recipient should not be able to discover the sender's view on
// what the filename even is!  However, the recipient must ensure that
// it is authentically from the sender.
func (user *User) ReceiveFile(filename string, sender string,
	msgid string) error {

	sharingMarsh, err := hex.DecodeString(msgid)
	if err != nil {
		return errors.New("Msgid corrupted")
	}

	var sharing [][]byte
	err = json.Unmarshal(sharingMarsh, &sharing)
	if err != nil {
		return errors.New("'sharing' Unmarshalling failed")
	}

	// Retrieve sender's public key
	sendPubKey, status := userlib.KeystoreGet(sender)
	if !status {
		return errors.New("Sender not found")
	}

	// Retreive the Marshalled messaged struct from the encrypted chunks
	index := 0
	msgidMarsh := make([]byte, len(sharing))
	for index < len(sharing) {
		// RSA Asymmetric Key Decryption
		decryptedBlock, err := userlib.RSADecrypt(user.Privkey,
			sharing[index], []byte("Tag"))
		if err != nil {
			return errors.New("RSA Encryption of 'sharing' failed\n")
		}

		msgidMarsh = append(msgidMarsh, decryptedBlock...)
		index += 1
	}

	// Remove leading \x00 characters from inodeMarsh
	for msgidMarsh[0] == []byte("\x00")[0] {
		msgidMarsh = remove(msgidMarsh, 0)
	}

	recv_info := struct {
		Collected_info []byte
		Signature      []byte
	}{}
	err = json.Unmarshal(msgidMarsh, &recv_info)
	if err != nil {
		return errors.New("Received Info Unmarshalling failed")
	}

	// Verify the Integrity of "sharing" message
	err = userlib.RSAVerify(&sendPubKey, recv_info.Collected_info,
		recv_info.Signature)
	if err != nil {
		return errors.New("Msgid has been tampered")
	}

	collected_info := struct {
		SymmKey      []byte
		ShRecordAddr string
	}{}
	err = json.Unmarshal(recv_info.Collected_info, &collected_info)
	if err != nil {
		return errors.New("Recieved Info Unmarshalling failed")
	}

	///////////////////////////////////////
	//           INODE STRUCTURE         //
	///////////////////////////////////////
	fileKey := user.GetInodeKey(filename)

	//
	// Initialize the Inode structure without any signature (at the moment)
	//

	// Generate a random Initialization Vector and random address for
	// encryption of SharingRecord Structure
	iv := make([]byte, BlockSize)
	copy(iv, userlib.RandomBytes(BlockSize))

	//
	// Here, after verifying the integrity of SharingRecord structure,
	// We add the recieved info about its address and symmetric keys
	// in the new inode
	randbyte := collected_info.SymmKey
	address := collected_info.ShRecordAddr

	file := &Inode_r{
		KeyAddr: fileKey, // The key at which this struct will be stored
		Inode: Inode{
			Filename:     filename,
			ShRecordAddr: address,
			SymmKey:      randbyte[:16],
		},
	}

	// Store the signature of Inode_r.Inode in Inode_r.Signature
	fileMarsh, err := json.Marshal(file.Inode)
	if err != nil {
		return errors.New("Inode_r.Inode Marshalling failed")
	}

	file.Signature, err = userlib.RSASign(user.Privkey, fileMarsh)
	if err != nil {
		return errors.New("RSA Signing of Inode_r.Inode failed")
	}

	// Finally, encrypt the whole Inode_r struct with User's Public key
	inodeMarsh, err := json.Marshal(file)
	if err != nil {
		return errors.New("Inode_r Marshalling failed")
	}

	// To store encrypted chunks
	var encrypted [][]byte
	var encryptedBlock []byte
	index = 0

	for index+190 <= len(inodeMarsh) {
		// RSA Asymmetric Key Encryption
		encryptedBlock, err = userlib.RSAEncrypt(&user.Privkey.PublicKey,
			inodeMarsh[index:index+190], []byte("Tag"))
		if err != nil {
			return errors.New("RSA Encryption of Inode_r failed\n")
		}
		index += 190
		encrypted = append(encrypted, encryptedBlock)
	}

	// In case the final chunk is not a multiple of 190
	encryptedBlock, err = userlib.RSAEncrypt(&user.Privkey.PublicKey,
		inodeMarsh[index:], []byte("Tag"))
	if err != nil {
		return errors.New("RSA Encryption of Inode_r failed\n")
	}
	encrypted = append(encrypted, encryptedBlock)

	encryptedMarsh, err := json.Marshal(encrypted)
	if err != nil {
		return errors.New("Marshalling of encrypted blocks failed")
	}

	// userlib.DatastoreDelete(fileKey)
	_, status = userlib.DatastoreGet(fileKey)
	if status {
		return errors.New("The specified file already exists")
	}

	userlib.DatastoreSet(fileKey, encryptedMarsh)

	return nil
}

// Removes access for all others.
func (user *User) RevokeFile(filename string) (err error) {
	///////////////////////////////////////
	//           INODE STRUCTURE         //
	///////////////////////////////////////
	fileKey := user.GetInodeKey(filename)

	// Retrieve the encrypted Inode structure from DataStore
	rsaEncrypted, status := userlib.DatastoreGet(fileKey)
	if status != true {
		return errors.New("Filename not found")
	}

	var encrypted [][]byte
	err = json.Unmarshal(rsaEncrypted, &encrypted)
	if err != nil {
		return errors.New("Inode_r Unmarshalling failed")
	}

	// Retreive the Marshalled Inode_r struct from the encrypted chunks
	index := 0
	inodeMarsh := make([]byte, len(encrypted))
	for index < len(encrypted) {
		// RSA Asymmetric Key Decryption
		decryptedBlock, err := userlib.RSADecrypt(user.Privkey,
			encrypted[index], []byte("Tag"))
		if err != nil {
			return errors.New("RSA Encryption of Inode_r failed\n")
		}

		inodeMarsh = append(inodeMarsh, decryptedBlock...)
		index += 1
	}

	// Remove leading \x00 characters from inodeMarsh
	for inodeMarsh[0] == []byte("\x00")[0] {
		inodeMarsh = remove(inodeMarsh, 0)
	}

	var file Inode_r
	err = json.Unmarshal(inodeMarsh, &file)
	if err != nil {
		return errors.New("Inode_r Unmarshalling failed")
	}

	// Verify Inode structure's integrity
	fileMarsh, err := json.Marshal(file.Inode)
	if err != nil {
		return errors.New("Inode_r.Inode Marshalling failed")
	}

	err = userlib.RSAVerify(&user.Privkey.PublicKey, fileMarsh, file.Signature)
	if err != nil {
		return errors.New("Inode Integrity Check failed")
	}

	//
	///////////////////////////////////////
	//      SHARINGRECORD STRUCTURE      //
	///////////////////////////////////////
	// Retrieve encrypted SharingRecord structure from DataStore
	shrCipher, status := userlib.DatastoreGet(file.Inode.ShRecordAddr)
	if !status {
		return errors.New("Sharing Record Structure can't be found")
	}

	iv := shrCipher[:BlockSize]
	cipher := userlib.CFBDecrypter(file.Inode.SymmKey, iv)

	// In place AES decryption of ciphertext
	cipher.XORKeyStream(shrCipher[BlockSize:], shrCipher[BlockSize:])

	var shrecord SharingRecord_r
	err = json.Unmarshal(shrCipher[BlockSize:], &shrecord)
	if err != nil {
		return errors.New("SharingRecord_r Unmarshalling failed")
	}

	// Verify the integrity of SharingRecord structure
	shrMarsh, err := json.Marshal(shrecord.SharingRecord)
	if err != nil {
		return errors.New("SharingRecord_r.SharingRecord Marshalling failed")
	}
	mac := userlib.NewHMAC(file.Inode.SymmKey)
	mac.Write(shrMarsh)
	if !userlib.Equal(shrecord.Signature, mac.Sum(nil)) {
		return errors.New("SharingRecord Integrity check failed")
	}

	//
	// Main Part of RevokeFile, change the encryption key of SharingRecord
	// structure (and also it's address)

	newKey, err := json.Marshal(userlib.RandomBytes(BlockSize))
	newAddr := hex.EncodeToString(newKey[:16])
	if err != nil {
		return errors.New("New-key marshalling failed")
	}

	prevAddr := file.Inode.ShRecordAddr

	// Update the key and address value in Inode struct
	file.Inode.SymmKey = newKey[:16]
	file.Inode.ShRecordAddr = newAddr

	// IMP
	// Need to store the updated addresses of Inode
	// Store the signature of Inode_r.Inode in Inode_r.Signature
	fileMarsh1, err := json.Marshal(file.Inode)
	if err != nil {
		return errors.New("File Inode marshalling failed")
	}

	file.Signature, err = userlib.RSASign(user.Privkey, fileMarsh1)
	if err != nil {
		return errors.New("File signing unsuccessful")
	}

	// Finally, encrypt the whole Inode_r struct with User's Public key
	inodeMarsh1, err := json.Marshal(file)
	if err != nil {
		return errors.New("Inode_r marshalling failed")
	}

	// To store encrypted chunks
	var encrypted1 [][]byte
	var encryptedBlock1 []byte
	index = 0

	for index+190 <= len(inodeMarsh1) {
		// RSA Asymmetric Key Encryption
		encryptedBlock1, err = userlib.RSAEncrypt(&user.Privkey.PublicKey,
			inodeMarsh1[index:index+190], []byte("Tag"))
		if err != nil {
			return
		}
		index += 190
		encrypted1 = append(encrypted1, encryptedBlock1)
	}

	// In case the final chunk is not a multiple of 190
	encryptedBlock1, err = userlib.RSAEncrypt(&user.Privkey.PublicKey,
		inodeMarsh1[index:], []byte("Tag"))
	if err != nil {
		return
	}
	encrypted1 = append(encrypted1, encryptedBlock1)

	encryptedMarsh1, err := json.Marshal(encrypted1)
	if err != nil {
		return
	}

	// Update the addresses of every data block
	numBlocks := len(shrecord.SharingRecord.SymmKey)
	for i := 0; i < numBlocks; i += 1 {
		// Bring in the blocks, verify their integrity, and place them
		// somewhere else in the DataStore
		dbKey := shrecord.SharingRecord.Address[i] // TODO: Change the loc
		symmKey := shrecord.SharingRecord.SymmKey[i]

		ciphertext, status := userlib.DatastoreGet(dbKey)
		if status != true {
			return errors.New("Data block not found")
		}

		iv := ciphertext[:BlockSize]
		cipher := userlib.CFBDecrypter(symmKey, iv)

		// In place AES decryption of ciphertext
		cipher.XORKeyStream(ciphertext[BlockSize:],
			ciphertext[BlockSize:])

		var data Data
		err = json.Unmarshal(ciphertext[BlockSize:], &data)
		if err != nil {
			return errors.New("Data block Unmarshalling failed")
		}

		// Check the data integrity
		mac := userlib.NewHMAC(symmKey)
		mac.Write(data.Value)
		hmacSum := mac.Sum(nil)
		if !userlib.Equal(data.Signature, hmacSum) {
			return errors.New("Data Integrity check failed")
		}

		// Key-value swap check
		if dbKey != data.KeyAddr {
			return errors.New("Key Value swap detected")
		}

		// New address for the block
		randbyte, _ := json.Marshal(userlib.RandomBytes(BlockSize))
		// randbyte, _ = json.Marshal(randbyte)
		address := hex.EncodeToString(randbyte[:16])

		shrecord.SharingRecord.Address[i] = address
		data.KeyAddr = address

		//
		// Re-encrypt the data block
		dblockMarsh, err := json.Marshal(data)
		if err != nil {
			return errors.New("Data block marshalling failed")
		}

		cipherdata := make([]byte, BlockSize+len(dblockMarsh))
		iv = cipherdata[:BlockSize]
		copy(iv, userlib.RandomBytes(BlockSize))

		// NOTE: The "key" needs to be of 16 bytes
		cipher = userlib.CFBEncrypter(symmKey, iv) // Check [:16]
		cipher.XORKeyStream(cipherdata[BlockSize:], []byte(dblockMarsh))

		// Data blocks stored at different locations
		userlib.DatastoreDelete(dbKey)
		userlib.DatastoreDelete(address)
		userlib.DatastoreSet(address, cipherdata)

	}

	// HMAC Signature via symmetric keys
	// Store the signature of SharingRecord_r.SharingRecord in Signature
	shrMarsh, err = json.Marshal(shrecord.SharingRecord)
	if err != nil {
		return errors.New("SharingRecord_r.SharingRecord Marshalling failed")
	}
	mac = userlib.NewHMAC(file.Inode.SymmKey)
	mac.Write(shrMarsh)
	shrecord.Signature = mac.Sum(nil)

	// Finally, encrypt the whole SharingRecord_r structure
	shrecord_rMarsh, err := json.Marshal(shrecord)
	if err != nil {
		return errors.New("SharingRecord_r Marshalling failed")
	}

	ciphertext := make([]byte, BlockSize+len(shrecord_rMarsh))
	iv = ciphertext[:BlockSize]
	copy(iv, userlib.RandomBytes(BlockSize))

	// NOTE: The "key" needs to be of 16 bytes
	cipher = userlib.CFBEncrypter(file.Inode.SymmKey, iv) // Check [:16]
	cipher.XORKeyStream(ciphertext[BlockSize:], []byte(shrecord_rMarsh))

	//
	// Push the RSA Encrypted Inode structure to Data Store
	userlib.DatastoreDelete(fileKey)
	userlib.DatastoreSet(fileKey, encryptedMarsh1)

	// Push the AES-CFB Encrypted SharingRecord structure to Data Store
	userlib.DatastoreDelete(file.Inode.ShRecordAddr)
	userlib.DatastoreSet(file.Inode.ShRecordAddr, ciphertext)

	// Delete previous values
	userlib.DatastoreDelete(prevAddr)
	return nil
}
