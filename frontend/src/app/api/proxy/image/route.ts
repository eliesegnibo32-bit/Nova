import { NextRequest, NextResponse } from "next/server";
import { S3Client, GetObjectCommand } from "@aws-sdk/client-s3";
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";

// Cache du client S3 (singleton)
let s3Client: S3Client | null = null;

function getS3Client(): S3Client {
  if (s3Client) return s3Client;
  const accountId = process.env.R2_ACCOUNT_ID;
  const accessKeyId = process.env.R2_ACCESS_KEY_ID;
  const secretAccessKey = process.env.R2_SECRET_ACCESS_KEY;
  const endpoint = process.env.R2_ENDPOINT || `https://${accountId}.r2.cloudflarestorage.com`;

  s3Client = new S3Client({
    region: "auto",
    endpoint,
    credentials: {
      accessKeyId: accessKeyId!,
      secretAccessKey: secretAccessKey!,
    },
  });
  return s3Client;
}

/**
 * Route proxy pour servir les images depuis Cloudflare R2.
 *
 * GET /api/proxy/image?key=shops/xxx/products/yyy.png
 *
 * Télécharge l'image depuis R2 via l'API S3 (avec credentials)
 * et la sert au navigateur. Cache de 1h côté navigateur.
 *
 * Alternative : si R2_PUBLIC_BASE_URL est configuré (accès public activé
 * dans le dashboard Cloudflare), le frontend utilise directement cette URL
 * sans passer par ce proxy.
 */
export async function GET(req: NextRequest) {
  const key = req.nextUrl.searchParams.get("key");
  if (!key) {
    return NextResponse.json({ error: "missing key" }, { status: 400 });
  }

  const bucket = process.env.R2_BUCKET || "nova-media";
  const publicBaseURL = process.env.R2_PUBLIC_BASE_URL;

  // Optimisation : si une URL publique R2 est configurée, rediriger vers elle
  if (publicBaseURL) {
    const publicUrl = `${publicBaseURL}/${key}`;
    return NextResponse.redirect(publicUrl, {
      status: 302,
      headers: { "cache-control": "public, max-age=3600" },
    });
  }

  // Sinon : télécharger l'image depuis R2 via l'API S3
  try {
    const client = getS3Client();
    const command = new GetObjectCommand({
      Bucket: bucket,
      Key: key,
    });

    // Générer une URL présignée (valide 5 min) et télécharger l'image
    const signedUrl = await getSignedUrl(client, command, { expiresIn: 300 });
    const imgRes = await fetch(signedUrl);

    if (!imgRes.ok) {
      // Image introuvable → placeholder SVG
      return placeholderResponse();
    }

    const body = await imgRes.arrayBuffer();
    const contentType = imgRes.headers.get("content-type") || "application/octet-stream";

    return new NextResponse(body, {
      status: 200,
      headers: {
        "content-type": contentType,
        "cache-control": "public, max-age=3600, s-maxage=86400",
      },
    });
  } catch (err) {
    console.error("[proxy/image] R2 fetch error:", err);
    return placeholderResponse();
  }
}

function placeholderResponse() {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200" viewBox="0 0 200 200"><rect fill="#f3f4f6" width="200" height="200"/><text x="100" y="100" font-family="sans-serif" font-size="14" fill="#9ca3af" text-anchor="middle" dy=".3em">Image</text></svg>`;
  return new NextResponse(svg, {
    status: 200,
    headers: { "content-type": "image/svg+xml", "cache-control": "no-cache" },
  });
}
