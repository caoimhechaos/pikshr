/*
 * (c) 2014, Tonnerre Lombard <tonnerre@ancient-solutions.com>,
 *	     Ancient Solutions. All rights reserved.
 *
 * Redistribution and use in source  and binary forms, with or without
 * modification, are permitted  provided that the following conditions
 * are met:
 *
 * * Redistributions of  source code  must retain the  above copyright
 *   notice, this list of conditions and the following disclaimer.
 * * Redistributions in binary form must reproduce the above copyright
 *   notice, this  list of conditions and the  following disclaimer in
 *   the  documentation  and/or  other  materials  provided  with  the
 *   distribution.
 * * Neither  the  name  of  Ancient Solutions  nor  the  name  of its
 *   contributors may  be used to endorse or  promote products derived
 *   from this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS"  AND ANY EXPRESS  OR IMPLIED WARRANTIES  OF MERCHANTABILITY
 * AND FITNESS  FOR A PARTICULAR  PURPOSE ARE DISCLAIMED. IN  NO EVENT
 * SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT,
 * INDIRECT, INCIDENTAL, SPECIAL,  EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 * (INCLUDING, BUT NOT LIMITED  TO, PROCUREMENT OF SUBSTITUTE GOODS OR
 * SERVICES; LOSS OF USE,  DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
 * STRICT  LIABILITY,  OR  TORT  (INCLUDING NEGLIGENCE  OR  OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED
 * OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package main

import (
	"bytes"
	"crypto/sha1"
	"database/cassandra"
	"encoding/hex"
	"errors"
	"github.com/nfnt/resize"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"time"
)

// Database access routines for the image database.
type PikShrDB struct {
	db *cassandra.RetryCassandraClient
}

type Picture struct {
	Id          string
	Contents    []byte
	ContentType string
	Title       string
	Description string
	AltText     string
}

var Err_ImageNotFound = errors.New("Image not found")

// Connect to the picture database.
func NewPikShrDB(server, keyspace string) (*PikShrDB, error) {
	var ire *cassandra.InvalidRequestException
	var client *cassandra.RetryCassandraClient
	var err error

	client, err = cassandra.NewRetryCassandraClient(server)
	if err != nil {
		return nil, err
	}

	ire, err = client.SetKeyspace(keyspace)
	if ire != nil {
		return nil, errors.New(ire.Why)
	}
	if err != nil {
		return nil, err
	}

	return &PikShrDB{
		db: client,
	}, nil
}

// Retrieve image data and metadata for the given ID.
// Image data will be taken from "column".
func (p *PikShrDB) getPictureColumn(id, column string) (*Picture, error) {
	var r []*cassandra.ColumnOrSuperColumn
	var scol *cassandra.ColumnOrSuperColumn
	var ret *Picture = new(Picture)
	var cp *cassandra.ColumnParent = cassandra.NewColumnParent()
	var pred *cassandra.SlicePredicate = cassandra.NewSlicePredicate()
	var ire *cassandra.InvalidRequestException
	var ue *cassandra.UnavailableException
	var te *cassandra.TimedOutException
	var key []byte
	var err error

	cp.ColumnFamily = "picture"
	pred.ColumnNames = [][]byte{
		[]byte("title"), []byte("description"), []byte("content_type"),
		[]byte("alt_text"),
	}

	if len(column) > 0 {
		pred.ColumnNames = append(pred.ColumnNames, []byte(column))
	}

	ret.Id = id
	key, err = hex.DecodeString(id)
	if err != nil {
		return nil, err
	}

	r, ire, ue, te, err = p.db.GetSlice(
		key, cp, pred, cassandra.ConsistencyLevel_ONE)
	if ire != nil {
		return nil, errors.New(ire.Why)
	}
	if ue != nil {
		return nil, errors.New("Unavailable")
	}
	if te != nil {
		return nil, errors.New("Timed out")
	}
	if err != nil {
		return nil, err
	}

	if len(r) == 0 {
		return nil, Err_ImageNotFound
	}

	for _, scol = range r {
		var col = scol.Column
		var cname string = string(col.Name)

		if cname == "title" {
			ret.Title = string(col.Value)
		} else if cname == "description" {
			ret.Description = string(col.Value)
		} else if cname == "content_type" {
			ret.ContentType = string(col.Value)
		} else if cname == "alt_text" {
			ret.AltText = string(col.Value)
		} else if len(column) > 0 && cname == column {
			ret.Contents = make([]byte, len(col.Value))
			copy(ret.Contents, col.Value)
		}
	}

	return ret, nil
}

// Retrieve the picture with the given ID from the database.
func (p *PikShrDB) GetPicture(id string) (*Picture, error) {
	return p.getPictureColumn(id, "picture")
}

// Retrieve the thumbnail with the given ID from the database.
func (p *PikShrDB) GetThumbnail(id string) (*Picture, error) {
	return p.getPictureColumn(id, "thumbnail")
}

// Retrieve only the metadata of the picture.
func (p *PikShrDB) GetMetadata(id string) (*Picture, error) {
	return p.getPictureColumn(id, "")
}

func makeMutation(name string, value []byte, now time.Time) (ret *cassandra.Mutation) {
	var cos = cassandra.NewColumnOrSuperColumn()
	var col = cassandra.NewColumn()

	col.Name = []byte(name)
	col.Value = value
	col.Timestamp = now.UnixNano()

	cos.Column = col

	ret = cassandra.NewMutation()
	ret.ColumnOrSupercolumn = cos
	return
}

// Insert the given picture and metadata into the database.
// Also, generate a thumbnail.
func (p *PikShrDB) InsertPicture(pic *Picture, creator string) (string, error) {
	var mutation *cassandra.Mutation
	var ire *cassandra.InvalidRequestException
	var ue *cassandra.UnavailableException
	var te *cassandra.TimedOutException
	var mmap = make(map[string]map[string][]*cassandra.Mutation)
	var mlist []*cassandra.Mutation
	var now = time.Now()
	var img, thumbnail image.Image
	var buf *bytes.Buffer = new(bytes.Buffer)
	var rd *bytes.Reader = bytes.NewReader(pic.Contents)
	var keystr string
	var key [sha1.Size]byte
	var err error

	// First, let's read and parse the image.
	img, _, err = image.Decode(rd)
	if err != nil {
		return "", err
	}

	// Write the full image out as a png.
	err = png.Encode(buf, img)
	if err != nil {
		return "", err
	}

	// Compute a hash of the image data.
	key = sha1.Sum(buf.Bytes())
	keystr = hex.EncodeToString(key[:])

	mmap[string(key[:])] = make(map[string][]*cassandra.Mutation)
	mlist = make([]*cassandra.Mutation, 0)

	mutation = makeMutation("picture", buf.Bytes(), now)
	mlist = append(mlist, mutation)

	// Create a thumbnail and save it too.
	thumbnail = resize.Thumbnail(200, 200, img, resize.Lanczos3)
	err = png.Encode(buf, thumbnail)
	if err != nil {
		return keystr, err
	}
	img = nil

	// Write the thumbnail out as a png.
	buf = new(bytes.Buffer)
	err = png.Encode(buf, thumbnail)
	if err != nil {
		return keystr, err
	}
	thumbnail = nil

	mutation = makeMutation("thumbnail", buf.Bytes(), now)
	mlist = append(mlist, mutation)

	// Now for some metadata.
	mutation = makeMutation("title", []byte(pic.Title), now)
	mlist = append(mlist, mutation)

	mutation = makeMutation("description", []byte(pic.Description), now)
	mlist = append(mlist, mutation)

	mutation = makeMutation("owner", []byte(creator), now)
	mlist = append(mlist, mutation)

	mutation = makeMutation("alt_text", []byte(pic.AltText), now)
	mlist = append(mlist, mutation)

	mmap[string(key[:])]["picture"] = mlist

	// Now: write it!
	ire, ue, te, err = p.db.AtomicBatchMutate(mmap, cassandra.ConsistencyLevel_QUORUM)
	if ire != nil {
		return keystr, errors.New(ire.Why)
	}
	if ue != nil {
		return keystr, errors.New("Unavailable")
	}
	if te != nil {
		return keystr, errors.New("Timed out")
	}
	return keystr, err
}
