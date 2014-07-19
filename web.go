/*
 * (c) 2014, Caoimhe Chaos <caoimhechaos@protonmail.com>,
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
	"html/template"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"strings"

	"ancient-solutions.com/ancientauth"
)

type WebPikShrService struct {
	auth     *ancientauth.Authenticator
	db       *PikShrDB
	skel     *template.Template
	upload   *template.Template
	num_pics int32
	num_own  int32
}

type webMetadata struct {
	User       string
	UploadedId string
	OwnPics    []*Picture
	AllPics    []*Picture
}

// Either allow people to upload new pictures, or serve existing ones.
func (w *WebPikShrService) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	var mfh *multipart.FileHeader
	var mf multipart.File
	var wmd webMetadata
	var res *Picture
	var err error

	if req.RequestURI == "/favicon.ico" {
		http.NotFound(rw, req)
		return
	} else if strings.HasSuffix(req.RequestURI, ".png") {
		var id string

		if strings.HasSuffix(req.RequestURI, ".thumb.png") {
			id = req.RequestURI[1 : len(req.RequestURI)-10]
			res, err = w.db.GetThumbnail(id)
		} else {
			id = req.RequestURI[1 : len(req.RequestURI)-4]
			res, err = w.db.GetPicture(id)
		}
		if err == Err_ImageNotFound {
			// TODO(caoimhe): this wants to be a template.
			http.NotFound(rw, req)
			return
		}
		if err != nil {
			log.Print("Unable to retrieve image ", id, ": ", err)
			// TODO(caoimhe): this wants to be a template.
			rw.WriteHeader(http.StatusInternalServerError)
			rw.Write([]byte(err.Error()))
			return
		}
		if len(res.Contents) == 0 {
			// TODO(caoimhe): this wants to be a template.
			http.NotFound(rw, req)
			return
		}

		rw.Header().Set("Content-Disposition", "inline")
		rw.Header().Set("Content-Type", "image/png")
		_, err = rw.Write(res.Contents)
		if err != nil {
			log.Print("Error writing out image ", id, ": ", err)
		}
		return
	} else if req.RequestURI != "/" {
		var md *Picture
		md, err = w.db.GetMetadata(req.RequestURI[1:])
		if err == Err_ImageNotFound {
			// TODO(caoimhe): this wants to be a template.
			http.NotFound(rw, req)
			return
		}
		if err != nil {
			log.Print("Unable to fetch metadata for ", req.RequestURI[1:],
				":", err)
			// TODO(caoimhe): this wants to be a template.
			rw.WriteHeader(http.StatusInternalServerError)
			rw.Write([]byte(err.Error()))
			return
		}
		err = w.skel.Execute(rw, md)
		if err != nil {
			rw.Write([]byte(err.Error()))
			log.Print("Error executing skeleton template: ", err)
		}
		return
	}

	wmd.User = w.auth.GetAuthenticatedUser(req)
	mf, mfh, err = req.FormFile("imageupload")
	if err != nil {
		log.Print("Unable to retrieve uploaded file: ", err)
	} else if len(wmd.User) > 0 {
		res = new(Picture)
		res.ContentType = mfh.Header["Content-Type"][0]
		res.AltText = req.FormValue("alt")
		res.Description = req.FormValue("description")
		res.Title = req.FormValue("title")
		res.Contents, err = ioutil.ReadAll(mf)
		if err != nil {
			log.Print("Unable to read multipart upload file: ", err)
		} else {
			wmd.UploadedId, err = w.db.InsertPicture(res, wmd.User)
			if err != nil {
				log.Print("Error storing ", mfh.Filename, ": ",
					err)
			}
		}
		mf.Close()
	} else if req.FormValue("ensure") == "authenticated" {
		w.auth.RequestAuthorization(rw, req)
		return
	}

	if req.FormValue("outform") == "json" {
		rw.Write([]byte(wmd.UploadedId))
		return
	}
	if len(wmd.User) > 0 {
		wmd.OwnPics, err = w.db.GetRecentPics(wmd.User, w.num_own)
		if err != nil {
			log.Print("Error determining the most recent pics of ",
				wmd.User, ": ", err)
		}
	}
	wmd.AllPics, err = w.db.GetRecentPics("", w.num_pics)
	if err != nil {
		log.Print("Error determining the most recent pics of ",
			wmd.User, ": ", err)
	}

	err = w.upload.Execute(rw, &wmd)
	if err != nil {
		rw.Write([]byte(err.Error()))
		log.Print("Unable to execute upload template: ", err)
	}
}
