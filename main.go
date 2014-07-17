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
	"flag"
	"html/template"
	"log"
	"net/http"

	"ancient-solutions.com/ancientauth"
)

func main() {
	var app_name, cert_file, key_file, ca_bundle, authserver string
	var bind, dbserver, dbname, skel_path, upload_path, static_path string
	var num_pics, num_own int
	var skel, upload *template.Template
	var auth *ancientauth.Authenticator
	var db *PikShrDB
	var err error

	flag.StringVar(&app_name, "app-name", "PicShr image sharing",
		"Application name to present to the authentication server")
	flag.StringVar(&cert_file, "cert", "pikshr.crt",
		"Service certificate for PikShr")
	flag.StringVar(&key_file, "priv", "pikshr.key",
		"Private serivce key for PikShr")
	flag.StringVar(&ca_bundle, "ca", "ca.crt",
		"Path to CA certificates to authenticate the login service")
	flag.StringVar(&authserver, "auth-server", "login.ancient-solutions.com",
		"Host name of the authentication service to use")

	flag.StringVar(&skel_path, "skelelton-template", "skel.html",
		"Path to the skeleton template to use for displaying the pictures")
	flag.StringVar(&upload_path, "upload-template", "upload.html",
		"Path to the upload template which is essentially the main page")
	flag.StringVar(&static_path, "static-path", ".",
		"Path to the required static files for the web interface")

	flag.StringVar(&bind, "bind", "[::]:8080",
		"host:port pair to bind the web server to")
	flag.StringVar(&dbserver, "cassandra-server", "localhost:9160",
		"Host name of the Cassandra database server to use")
	flag.StringVar(&dbname, "cassandra-dbname", "pikshr",
		"Name of the Cassandra keyspace the images etc. are stored in")

	flag.IntVar(&num_pics, "pics-per-page", 32,
		"Number of pictures to display on the home page")
	flag.IntVar(&num_own, "own-pics-per-page", 8,
		"Number of _own_ pictures to display on the home page")

	flag.Parse()

	skel, err = template.ParseFiles(skel_path)
	if err != nil {
		log.Fatal("Unable to parse template ", skel_path, ": ", err)
	}
	upload, err = template.ParseFiles(upload_path)
	if err != nil {
		log.Fatal("Unable to parse template ", upload_path, ": ", err)
	}

	auth, err = ancientauth.NewAuthenticator(
		app_name, cert_file, key_file, ca_bundle, authserver)
	if err != nil {
		log.Fatal("Error creating authenticator: ", err)
	}

	db, err = NewPikShrDB(dbserver, dbname)
	if err != nil {
		log.Fatal("Error connecting to database ", dbname, " at ",
			dbserver, ": ", err)
	}

	http.Handle("/css/", http.FileServer(http.Dir(static_path)))
	http.Handle("/js/", http.FileServer(http.Dir(static_path)))
	http.Handle("/fonts/", http.FileServer(http.Dir(static_path)))

	http.Handle("/", &WebPikShrService{
		auth:     auth,
		db:       db,
		skel:     skel,
		upload:   upload,
		num_pics: int32(num_pics),
		num_own:  int32(num_own),
	})
	err = http.ListenAndServe(bind, nil)
	if err != nil {
		log.Fatal("Error serving HTTP on ", bind, ": ", err)
	}
}
