import { NextRequest, NextResponse } from "next/server";
import { S3Client, PutObjectCommand } from "@aws-sdk/client-s3";
import { randomUUID } from "crypto";

// Cache du client S3 (singleton)
let s3Client: S3Client | null = null;

function getS3Client(): S3Client {
  if (s3Client) return s3Client;
  const accountId = process.env.R2_ACCOUNT_ID;
  const accessKeyId = process.env.R2_ACCESS_KEY_ID;
  const secretAccessKey = process.env.R2_SECRET_ACCESS_KEY;
  const endpoint =
    process.env.R2_ENDPOINT || `https://${accountId}.r2.cloudflarestorage.com`;

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
 * Route d'upload d'image vers Cloudflare R2.
 *
 * POST /api/upload-image
 * Body: multipart/form-data avec "file" (image) et "shopId" (string)
 *
 * Le backend Go sur Render upload vers son propre R2 (autres credentials).
 * Cette route upload directement vers LE R2 du client (nos credentials),
 * ce qui garantit que l'image sera accessible via /api/proxy/image.
 *
 * Response: { ok: true, key: "...", url: "/api/proxy/image?key=..." }
 */
export async function POST(req: NextRequest) {
  try {
    const formData = await req.formData();
    const file = formData.get("file") as File | null;
    const shopId = (formData.get("shopId") as string) || "general";
    const category = (formData.get("category") as string) || "product";

    if (!file) {
      return NextResponse.json(
        { error: "missing_file", message: "Champ file requis" },
        { status: 400 },
      );
    }

    // Validation du type
    const validTypes = ["image/jpeg", "image/jpg", "image/png", "image/webp"];
    if (!validTypes.includes(file.type)) {
      return NextResponse.json(
        {
          error: "invalid_type",
          message: `Type non supporté: ${file.type}. Acceptés: JPEG, PNG, WebP.`,
        },
        { status: 400 },
      );
    }

    // Validation de la taille (5 Mo max)
    if (file.size > 5 * 1024 * 1024) {
      return NextResponse.json(
        { error: "too_large", message: "Taille maximum : 5 Mo." },
        { status: 400 },
      );
    }

    // Génère la clé R2
    const ext = file.name.split(".").pop()?.toLowerCase() || "png";
    const uuid = randomUUID();
    const key = `shops/${shopId}/${category}s/${uuid}.${ext}`;

    // Upload vers R2
    const client = getS3Client();
    const body = Buffer.from(await file.arrayBuffer());

    await client.send(
      new PutObjectCommand({
        Bucket: process.env.R2_BUCKET || "nova-media",
        Key: key,
        Body: body,
        ContentType: file.type,
      }),
    );

    // Retourne l'URL proxy (pas d'URL publique R2 car non configurée)
    const proxyUrl = `/api/proxy/image?key=${encodeURIComponent(key)}`;

    return NextResponse.json({
      ok: true,
      key,
      url: proxyUrl,
      size: body.length,
      content_type: file.type,
    });
  } catch (err: any) {
    console.error("[upload-image] Erreur:", err);
    return NextResponse.json(
      {
        error: "upload_failed",
        message: err?.message || "Échec du téléversement",
      },
      { status: 500 },
    );
  }
}
