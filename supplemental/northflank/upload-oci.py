"""Upload a single-platform OCI image to a reserved Northflank registry repository."""

import hashlib
import http.client
import json
import os
import re
import sys
import tarfile
import time
from urllib.parse import urlencode, urljoin, urlsplit


REGISTRY = "registry.northflank.com"
TRANSIENT_STATUS = {429, 500, 502, 503, 504}


def read_blob(archive, descriptor):
    digest = descriptor["digest"]
    if not re.fullmatch(r"sha256:[a-f0-9]{64}", digest):
        raise ValueError("OCI digest must use sha256")
    contents = archive.extractfile("blobs/sha256/" + digest.removeprefix("sha256:")).read()
    if len(contents) != descriptor["size"] or hashlib.sha256(contents).hexdigest() != digest[7:]:
        raise ValueError("OCI blob checksum does not match its descriptor")
    return contents


def registry_request(token, method, path, description, expected, body=b"", media_type=None):
    headers = {"Authorization": f"Bearer {token}"}
    if media_type:
        headers["Content-Type"] = media_type
    for delay in (0, 2, 5, 10, 20):
        if delay:
            time.sleep(delay)
        connection = http.client.HTTPSConnection(REGISTRY, timeout=45)
        try:
            connection.request(method, path, body=body, headers=headers)
            response = connection.getresponse()
            status = response.status
            location = response.getheader("Location")
        except (OSError, http.client.HTTPException):
            status = None
        finally:
            connection.close()

        if status == expected:
            return location
        if status is not None and status not in TRANSIENT_STATUS:
            raise RuntimeError(f"{description} returned HTTP {status}")
        print(f"{description} returned {'HTTP ' + str(status) if status else 'a connection error'}", flush=True)

    raise RuntimeError(f"{description} did not succeed after retries")


def upload_blob(token, repository, digest, body):
    upload_path = f"/v2/{repository}/blobs/uploads/"
    location = registry_request(token, "POST", upload_path, "Start blob upload", 202)
    if not location:
        raise RuntimeError("Registry did not return a blob upload URL")
    target = urlsplit(urljoin(f"https://{REGISTRY}{upload_path}", location))
    if target.scheme not in {"http", "https"} or target.netloc != REGISTRY or not target.path.startswith(upload_path):
        raise RuntimeError("Registry returned an unexpected blob upload URL")
    query = target.query + "&" if target.query else ""
    query += urlencode({"digest": digest})
    registry_request(token, "PUT", f"{target.path}?{query}", "Finish blob upload", 201,
                     body, "application/octet-stream")


def main(archive_path, repository, tag):
    if not re.fullmatch(r"[a-z0-9]+(?:[._/-][a-z0-9]+)*", repository):
        raise ValueError("Registry repository name is invalid")
    if not re.fullmatch(r"[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}", tag):
        raise ValueError("Registry tag is invalid")
    token = os.environ["NORTHFLANK_UPLOAD_TOKEN"]

    with tarfile.open(archive_path, "r:") as archive:
        descriptor, = json.load(archive.extractfile("index.json"))["manifests"]
        if descriptor["mediaType"] != "application/vnd.oci.image.manifest.v1+json":
            raise ValueError("Expected a single OCI image manifest")
        manifest_bytes = read_blob(archive, descriptor)
        manifest = json.loads(manifest_bytes)
        for blob in [manifest["config"], *manifest["layers"]]:
            upload_blob(token, repository, blob["digest"], read_blob(archive, blob))

    registry_request(token, "PUT", f"/v2/{repository}/manifests/{tag}", "Publish manifest", 201,
                     manifest_bytes, descriptor["mediaType"])


if __name__ == "__main__":
    try:
        main(*sys.argv[1:])
    except (KeyError, ValueError, RuntimeError, OSError, tarfile.TarError) as error:
        # Registry response bodies may contain credentials. Only report checked errors.
        sys.exit(f"Image upload failed: {error}")
