package firebase

import (
	"errors"
	"fmt"
	"time"
	"appengine"
	"appengine/memcache"

	"github.com/SermoDigital/jose/crypto"
	"github.com/SermoDigital/jose/jws"
	"github.com/SermoDigital/jose/jwt"
	"appengine/urlfetch"
)

// clientCertURL is the URL containing the public keys for the Google certs
// (whose private keys are used to sign Firebase Auth ID Tokens).
const clientCertURL = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"
const clientCertURLKEY = "CERT_CACHE.FIREBASE"

// defaultAcceptableExpSkew is the default expiry leeway.
const defaultAcceptableExpSkew = 300 * time.Second

func verify(ctx appengine.Context, projectID, tokenString string) (*Token, error) {
	decodedJWT, err := jws.ParseJWT([]byte(tokenString))
	if err != nil {
		return nil, err
	}
	decodedJWS, ok := decodedJWT.(jws.JWS)
	if !ok {
		return nil, errors.New("Firebase Auth ID Token cannot be decoded")
	}

	keys := func(j jws.JWS) ([]interface{}, error) {
		certs := &Certificates{URL: clientCertURL}

		var storeCache = true
		if item, err := memcache.Get(ctx, clientCertURLKEY); err == memcache.ErrCacheMiss {

		} else if err != nil {

		} else {
			cachedCert, _ := Parse(item.Value)
			certs.certs = cachedCert
			certs.exp = time.Now().Add(time.Duration(1) * time.Hour)
			storeCache = false
		}

		certs.ctx = ctx
		certs.Transport = &urlfetch.Transport{Context: ctx}

		kid, ok := j.Protected().Get("kid").(string)
		if !ok {
			return nil, errors.New("Firebase Auth ID Token has no 'kid' claim")
		}

		cert, err := certs.Cert(kid)
		if err != nil {
			return nil, err
		}

		if (storeCache) {
			item := &memcache.Item{
				Key:   clientCertURLKEY,
				Value: certs.certsBinary,
				Expiration: certs.exp.Sub(time.Now()),
			}

			memcache.Set(ctx, item)
		}

		return []interface{}{cert.PublicKey}, nil
	}

	err = decodedJWS.VerifyCallback(keys,
		[]crypto.SigningMethod{crypto.SigningMethodRS256},
		&jws.SigningOpts{Number: 1, Indices: []int{0}})
	if err != nil {
		return nil, err
	}

	ks, _ := keys(decodedJWS)
	key := ks[0]
	if err := decodedJWT.Validate(key, crypto.SigningMethodRS256, validator(projectID)); err != nil {
		return nil, err
	}

	return &Token{delegate: decodedJWT}, nil
}

func validator(projectID string) *jwt.Validator {
	v := &jwt.Validator{}
	v.EXP = defaultAcceptableExpSkew
	v.SetAudience(projectID)
	v.SetIssuer(fmt.Sprintf("https://securetoken.google.com/%s", projectID))
	v.Fn = func(claims jwt.Claims) error {
		subject, ok := claims.Subject()
		if !ok || len(subject) == 0 || len(subject) > 128 {
			return jwt.ErrInvalidSUBClaim
		}
		return nil
	}
	return v
}
