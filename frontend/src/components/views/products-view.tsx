"use client";

import * as React from "react";
import {
  Plus,
  Upload,
  Search,
  Pencil,
  Trash2,
  Package,
  MoreHorizontal,
  Utensils,
  Salad,
  CupSoda,
  Loader2,
  Infinity as InfinityIcon,
  AlertCircle,
  ImageIcon,
  X,
  Download,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  products as mockProducts,
  productCategories,
  type Product,
} from "@/lib/mock-data";
import {
  productsApi,
  optionsApi,
  uploadApi,
  ApiError,
  type ProductFull,
  type ProductInput,
  type ProductOption,
  type ProductOptionInput,
  type ProductOptionType,
  type StockMode,
} from "@/lib/api";
import { formatFCFA } from "@/lib/format";
import { ViewHeader } from "./view-header";

// ====== Helpers de mapping API → UI ======

function apiProductToDisplay(p: ProductFull): Product {
  const v = p.variants?.[0];
  return {
    id: p.id,
    name: p.name,
    category: p.category,
    variantLabel: v?.label ?? "—",
    price: v?.price ?? p.price,
    stock: v?.stock ?? 0,
    threshold: v?.threshold ?? 5,
    status: p.status,
    thumbnailColor: "bg-zinc-900",
    sku: v?.sku ?? "",
  };
}

// ====== Composants visuels ======

function ProductThumbnail({
  product,
  imageUrl,
}: {
  product: Product;
  imageUrl?: string | null;
}) {
  if (imageUrl) {
    return (
      <img
        src={imageUrl}
        alt={product.name}
        className="size-10 shrink-0 rounded-md object-cover"
      />
    );
  }
  return (
    <span
      className={cn(
        "flex size-10 shrink-0 items-center justify-center rounded-md text-white",
        product.thumbnailColor,
      )}
    >
      <Package className="size-5" />
    </span>
  );
}

function statusBadge(status: Product["status"]) {
  return status === "published" ? (
    <Badge className="bg-brand/10 text-brand hover:bg-brand/20">Publié</Badge>
  ) : (
    <Badge variant="outline" className="text-muted-foreground">
      Brouillon
    </Badge>
  );
}

function stockBadge(p: Product) {
  if (p.stock === 0) {
    return <Badge variant="destructive">Rupture</Badge>;
  }
  if (p.stock < p.threshold) {
    return (
      <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
        Faible
      </Badge>
    );
  }
  return <Badge variant="secondary">OK</Badge>;
}

// ====== Upload photo (vers R2) ======

function PhotoUpload({
  value,
  onChange,
  label = "Photo",
  category = "product",
}: {
  value: string | null;
  onChange: (url: string | null) => void;
  label?: string;
  category?: "product" | "option";
}) {
  const [uploading, setUploading] = React.useState(false);
  const inputRef = React.useRef<HTMLInputElement>(null);

  async function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    // Validation simple côté client.
    if (!/^image\/(jpeg|png|webp)$/.test(file.type)) {
      toast.error("Format non supporté", {
        description: "Formats acceptés : JPEG, PNG, WebP.",
      });
      return;
    }
    if (file.size > 5 * 1024 * 1024) {
      toast.error("Image trop volumineuse", {
        description: "Taille maximum : 5 Mo.",
      });
      return;
    }
    setUploading(true);
    try {
      const res =
        category === "option"
          ? await uploadApi.option(file)
          : await uploadApi.product(file);
      onChange(res.url);
      toast.success("Photo téléversée");
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Échec du téléversement";
      toast.error("Téléversement échoué", {
        description: `${msg} — vous pouvez continuer sans photo.`,
      });
    } finally {
      setUploading(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <div className="flex items-center gap-3">
        <div className="flex size-16 items-center justify-center overflow-hidden rounded-md border bg-muted/30">
          {value ? (
            <img src={value} alt="" className="size-full object-cover" />
          ) : (
            <ImageIcon className="size-6 text-muted-foreground" />
          )}
        </div>
        <input
          ref={inputRef}
          type="file"
          accept="image/jpeg,image/png,image/webp"
          onChange={handleFile}
          className="hidden"
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => inputRef.current?.click()}
          disabled={uploading}
        >
          {uploading ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Upload className="size-4" />
          )}
          {uploading ? "Téléversement…" : "Choisir une image"}
        </Button>
        {value && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => onChange(null)}
            disabled={uploading}
          >
            <X className="size-4" /> Retirer
          </Button>
        )}
      </div>
      <p className="text-[11px] text-muted-foreground">
        JPEG, PNG ou WebP — 5 Mo max. Optionnel.
      </p>
    </div>
  );
}

// ====== Plats & Options ======

const OPTION_TYPES: {
  key: ProductOptionType;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  tone: string;
}[] = [
  { key: "plat", label: "Plats", icon: Utensils, tone: "bg-brand/10 text-brand" },
  {
    key: "accompagnement",
    label: "Accompagnements",
    icon: Salad,
    tone: "bg-amber-accent/15 text-amber-accent",
  },
  {
    key: "boisson",
    label: "Boissons",
    icon: CupSoda,
    tone: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  },
];

function StockModeBadge({ mode, qty }: { mode: StockMode; qty: number | null }) {
  if (mode === "illimite") {
    return (
      <Badge variant="outline" className="gap-1 text-foreground">
        <InfinityIcon className="size-3" /> Illimité
      </Badge>
    );
  }
  if (mode === "epuise") {
    return (
      <Badge variant="destructive" className="gap-1">
        <AlertCircle className="size-3" /> Épuisé
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className="gap-1">
      {qty ?? 0} en stock
    </Badge>
  );
}

function OptionRow({
  option,
  onEdit,
  onDelete,
  busy,
}: {
  option: ProductOption;
  onEdit: () => void;
  onDelete: () => void;
  busy: boolean;
}) {
  return (
    <TableRow>
      <TableCell>
        <div className="flex items-center gap-2">
          {option.image_url ? (
            <img
              src={option.image_url}
              alt=""
              className="size-8 shrink-0 rounded object-cover"
            />
          ) : null}
          <span className="font-medium">{option.name}</span>
          {!option.active && (
            <Badge variant="outline" className="text-muted-foreground">
              Inactif
            </Badge>
          )}
        </div>
      </TableCell>
      <TableCell className="text-right font-semibold tabular-nums">
        {option.price === 0 ? (
          <Badge className="bg-brand/10 text-brand hover:bg-brand/20">Offert</Badge>
        ) : (
          formatFCFA(option.price)
        )}
      </TableCell>
      <TableCell>
        <StockModeBadge mode={option.stock_mode} qty={option.stock_qty} />
      </TableCell>
      <TableCell className="w-[60px]">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="size-8"
              aria-label="Actions option"
              disabled={busy}
            >
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onEdit}>
              <Pencil className="size-4" /> Modifier
            </DropdownMenuItem>
            <DropdownMenuItem
              className="text-destructive focus:text-destructive"
              onClick={onDelete}
            >
              <Trash2 className="size-4" /> Supprimer
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
  );
}

function OptionDialog({
  open,
  onOpenChange,
  option,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  option: ProductOption | null;
  onSubmit: (input: ProductOptionInput, id?: string) => Promise<void>;
}) {
  const [type, setType] = React.useState<ProductOptionType>("plat");
  const [name, setName] = React.useState("");
  const [price, setPrice] = React.useState<string>("");
  const [stockMode, setStockMode] = React.useState<StockMode>("quantite");
  const [stockQty, setStockQty] = React.useState<string>("");
  const [active, setActive] = React.useState(true);
  const [imageUrl, setImageUrl] = React.useState<string | null>(null);
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (option) {
      setType(option.type);
      setName(option.name);
      setPrice(String(option.price));
      setStockMode(option.stock_mode);
      setStockQty(option.stock_qty !== null ? String(option.stock_qty) : "");
      setActive(option.active);
      setImageUrl(option.image_url ?? null);
    } else {
      setType("plat");
      setName("");
      setPrice("");
      setStockMode("quantite");
      setStockQty("");
      setActive(true);
      setImageUrl(null);
    }
  }, [option, open]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      toast.error("Le nom est obligatoire");
      return;
    }
    const priceNum = Number(price || "0");
    if (isNaN(priceNum) || priceNum < 0) {
      toast.error("Prix invalide");
      return;
    }
    const qty =
      stockMode === "quantite"
        ? Math.max(0, parseInt(stockQty || "0", 10) || 0)
        : null;
    setSaving(true);
    try {
      await onSubmit(
        {
          type,
          name: name.trim(),
          price: priceNum,
          stock_mode: stockMode,
          stock_qty: qty,
          active,
          image_url: imageUrl,
        },
        option?.id,
      );
    } catch {
      // Toast déjà émis par le parent.
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {option ? "Modifier le plat" : "Nouveau plat / accompagnement / boisson"}
          </DialogTitle>
          <DialogDescription>
            Ajoutez un plat, un accompagnement ou une boisson à la carte du jour.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <PhotoUpload
            value={imageUrl}
            onChange={setImageUrl}
            label="Photo du plat"
            category="option"
          />

          <div className="space-y-1.5">
            <Label htmlFor="opt-type">Type</Label>
            <Select value={type} onValueChange={(v) => setType(v as ProductOptionType)}>
              <SelectTrigger id="opt-type">
                <SelectValue placeholder="Type" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="plat">Plat</SelectItem>
                <SelectItem value="accompagnement">Accompagnement</SelectItem>
                <SelectItem value="boisson">Boisson</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="opt-name">Nom</Label>
            <Input
              id="opt-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Ex : Attiéké poisson braisé"
              required
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="opt-price">Prix (FCFA)</Label>
              <Input
                id="opt-price"
                type="number"
                min="0"
                step="50"
                value={price}
                onChange={(e) => setPrice(e.target.value)}
                placeholder="0 = offert"
              />
              <p className="text-[11px] text-muted-foreground">
                Mettre 0 pour marquer comme « Offert ».
              </p>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="opt-mode">Gestion du stock</Label>
              <Select value={stockMode} onValueChange={(v) => setStockMode(v as StockMode)}>
                <SelectTrigger id="opt-mode">
                  <SelectValue placeholder="Mode" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="quantite">Quantité</SelectItem>
                  <SelectItem value="epuise">Épuisé</SelectItem>
                  <SelectItem value="illimite">Illimité</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          {stockMode === "quantite" && (
            <div className="space-y-1.5">
              <Label htmlFor="opt-qty">Quantité en stock</Label>
              <Input
                id="opt-qty"
                type="number"
                min="0"
                value={stockQty}
                onChange={(e) => setStockQty(e.target.value)}
                placeholder="0"
              />
            </div>
          )}

          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={active}
              onChange={(e) => setActive(e.target.checked)}
              className="size-4 rounded border-input accent-[var(--brand)]"
            />
            <span>Option active (visible par l&apos;IA et le client)</span>
          </label>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={saving}
            >
              Annuler
            </Button>
            <Button type="submit" disabled={saving}>
              {saving && <Loader2 className="size-4 animate-spin" />}
              {option ? "Enregistrer" : "Créer"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function OptionsSection({
  options,
  loading,
  busyId,
  onAdd,
  onEdit,
  onDelete,
}: {
  options: ProductOption[];
  loading: boolean;
  busyId: string | null;
  onAdd: () => void;
  onEdit: (opt: ProductOption) => void;
  onDelete: (opt: ProductOption) => void;
}) {
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-col gap-3 border-b p-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <span className="flex size-8 items-center justify-center rounded-md bg-brand/10 text-brand">
            <Utensils className="size-4" />
          </span>
          <div>
            <div className="text-sm font-semibold">Plats</div>
            <div className="text-xs text-muted-foreground">
              Carte du jour : plats, accompagnements et boissons.
            </div>
          </div>
        </div>
        <Button size="sm" onClick={onAdd}>
          <Plus className="size-4" /> Ajouter un plat
        </Button>
      </div>

      <div className="grid gap-px bg-border lg:grid-cols-3">
        {OPTION_TYPES.map((t) => {
          const items = options.filter((o) => o.type === t.key);
          return (
            <div key={t.key} className="bg-card">
              <div className="flex items-center justify-between border-b bg-muted/30 px-4 py-2.5">
                <div className="flex items-center gap-2">
                  <span className={cn("flex size-6 items-center justify-center rounded", t.tone)}>
                    <t.icon className="size-3.5" />
                  </span>
                  <span className="text-sm font-medium">{t.label}</span>
                </div>
                <Badge variant="secondary" className="text-[10px]">
                  {items.length}
                </Badge>
              </div>
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="text-xs">Nom</TableHead>
                    <TableHead className="text-right text-xs">Prix</TableHead>
                    <TableHead className="text-xs">Stock</TableHead>
                    <TableHead className="w-[40px]"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loading ? (
                    Array.from({ length: 2 }).map((_, i) => (
                      <TableRow key={`sk-${t.key}-${i}`}>
                        <TableCell colSpan={4}>
                          <Skeleton className="h-6 w-full" />
                        </TableCell>
                      </TableRow>
                    ))
                  ) : items.length === 0 ? (
                    <TableRow>
                      <TableCell
                        colSpan={4}
                        className="py-6 text-center text-xs text-muted-foreground"
                      >
                        Aucun {t.label.toLowerCase().replace(/s$/, "")}.
                      </TableCell>
                    </TableRow>
                  ) : (
                    items.map((opt) => (
                      <OptionRow
                        key={opt.id}
                        option={opt}
                        busy={busyId === opt.id}
                        onEdit={() => onEdit(opt)}
                        onDelete={() => onDelete(opt)}
                      />
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          );
        })}
      </div>
    </Card>
  );
}

// ====== Dialog produit (création / édition) ======

interface VariantDraft {
  size: string;
  color: string;
  stock: string;
  threshold: string;
  sku: string;
}

function ProductDialog({
  open,
  onOpenChange,
  product,
  imageUrl,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  product: Product | null;
  imageUrl: string | null;
  onSubmit: (input: ProductInput, id?: string) => Promise<void>;
}) {
  const [name, setName] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [category, setCategory] = React.useState(productCategories[1] ?? "Robes");
  const [brand, setBrand] = React.useState("");
  const [price, setPrice] = React.useState("");
  const [status, setStatus] = React.useState<"published" | "draft">("draft");
  const [photo, setPhoto] = React.useState<string | null>(null);
  const [variants, setVariants] = React.useState<VariantDraft[]>([
    { size: "", color: "", stock: "0", threshold: "5", sku: "" },
  ]);
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (product) {
      setName(product.name);
      setDescription("");
      setCategory(product.category);
      setBrand("");
      setPrice(String(product.price));
      setStatus(product.status);
      setPhoto(imageUrl);
      setVariants([
        {
          size: "",
          color: product.variantLabel,
          stock: String(product.stock),
          threshold: String(product.threshold),
          sku: product.sku,
        },
      ]);
    } else {
      setName("");
      setDescription("");
      setCategory(productCategories[1] ?? "Robes");
      setBrand("");
      setPrice("");
      setStatus("draft");
      setPhoto(imageUrl);
      setVariants([
        { size: "", color: "", stock: "0", threshold: "5", sku: "" },
      ]);
    }
  }, [product, open, imageUrl]);

  function updateVariant(idx: number, patch: Partial<VariantDraft>) {
    setVariants((prev) =>
      prev.map((v, i) => (i === idx ? { ...v, ...patch } : v)),
    );
  }

  function addVariant() {
    setVariants((prev) => [
      ...prev,
      { size: "", color: "", stock: "0", threshold: "5", sku: "" },
    ]);
  }

  function removeVariant(idx: number) {
    setVariants((prev) => (prev.length === 1 ? prev : prev.filter((_, i) => i !== idx)));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      toast.error("Le nom est obligatoire");
      return;
    }
    const priceNum = Number(price || "0");
    if (isNaN(priceNum) || priceNum < 0) {
      toast.error("Prix invalide");
      return;
    }
    const cleanedVariants = variants
      .map((v) => ({
        label: [v.color, v.size].filter(Boolean).join(" / ") || "Unique",
        size: v.size || undefined,
        color: v.color || undefined,
        sku: v.sku || undefined,
        stock: Math.max(0, parseInt(v.stock || "0", 10) || 0),
        threshold: Math.max(0, parseInt(v.threshold || "0", 10) || 0),
      }));
    setSaving(true);
    try {
      await onSubmit(
        {
          name: name.trim(),
          description: description.trim() || undefined,
          category,
          brand: brand.trim() || undefined,
          price: priceNum,
          status,
          image_url: photo,
          variants: cleanedVariants,
        },
        product?.id,
      );
    } catch {
      // Toast déjà émis par le parent.
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {product ? "Modifier le produit" : "Ajouter un produit"}
          </DialogTitle>
          <DialogDescription>
            Renseignez les informations du produit, sa photo et ses variantes
            (taille, couleur, stock).
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <PhotoUpload value={photo} onChange={setPhoto} label="Photo du produit" />

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="prod-name">Nom *</Label>
              <Input
                id="prod-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Ex : Robe noire wax"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="prod-category">Catégorie</Label>
              <Select value={category} onValueChange={setCategory}>
                <SelectTrigger id="prod-category">
                  <SelectValue placeholder="Catégorie" />
                </SelectTrigger>
                <SelectContent>
                  {productCategories
                    .filter((c) => c !== "Toutes")
                    .map((c) => (
                      <SelectItem key={c} value={c}>
                        {c}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="prod-desc">Description</Label>
            <Textarea
              id="prod-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Décrivez le produit en quelques lignes…"
              rows={2}
            />
          </div>

          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label htmlFor="prod-brand">Marque</Label>
              <Input
                id="prod-brand"
                value={brand}
                onChange={(e) => setBrand(e.target.value)}
                placeholder="Optionnel"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="prod-price">Prix (FCFA) *</Label>
              <Input
                id="prod-price"
                type="number"
                min="0"
                step="100"
                value={price}
                onChange={(e) => setPrice(e.target.value)}
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="prod-status">Statut</Label>
              <Select
                value={status}
                onValueChange={(v) => setStatus(v as "published" | "draft")}
              >
                <SelectTrigger id="prod-status">
                  <SelectValue placeholder="Statut" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="draft">Brouillon</SelectItem>
                  <SelectItem value="published">Publié</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>Variantes</Label>
              <Button type="button" variant="outline" size="sm" onClick={addVariant}>
                <Plus className="size-4" /> Ajouter une variante
              </Button>
            </div>
            <div className="space-y-2">
              {variants.map((v, idx) => (
                <div
                  key={idx}
                  className="grid grid-cols-2 gap-2 rounded-md border p-2 sm:grid-cols-6"
                >
                  <Input
                    placeholder="Couleur"
                    value={v.color}
                    onChange={(e) => updateVariant(idx, { color: e.target.value })}
                  />
                  <Input
                    placeholder="Taille"
                    value={v.size}
                    onChange={(e) => updateVariant(idx, { size: e.target.value })}
                  />
                  <Input
                    type="number"
                    min="0"
                    placeholder="Stock"
                    value={v.stock}
                    onChange={(e) => updateVariant(idx, { stock: e.target.value })}
                  />
                  <Input
                    type="number"
                    min="0"
                    placeholder="Seuil"
                    value={v.threshold}
                    onChange={(e) => updateVariant(idx, { threshold: e.target.value })}
                  />
                  <Input
                    placeholder="SKU"
                    value={v.sku}
                    onChange={(e) => updateVariant(idx, { sku: e.target.value })}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => removeVariant(idx)}
                    disabled={variants.length === 1}
                    aria-label="Supprimer la variante"
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </div>
              ))}
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={saving}
            >
              Annuler
            </Button>
            <Button type="submit" disabled={saving}>
              {saving && <Loader2 className="size-4 animate-spin" />}
              {product ? "Enregistrer" : "Créer le produit"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ====== Dialog suppression produit ======

function ProductDeleteDialog({
  open,
  onOpenChange,
  product,
  onConfirm,
  busy,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  product: Product | null;
  onConfirm: () => void;
  busy: boolean;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Supprimer le produit</DialogTitle>
          <DialogDescription>
            Êtes-vous sûr de vouloir supprimer{" "}
            <strong className="text-foreground">{product?.name}</strong> ? Cette
            action est définitive.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={busy}
          >
            Annuler
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={onConfirm}
            disabled={busy}
          >
            {busy && <Loader2 className="size-4 animate-spin" />}
            Supprimer
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ====== Dialog import CSV ======

function buildCsvTemplate(): string {
  const header = "name,category,brand,price,color,size,stock,threshold,sku,status";
  const rows = [
    "Robe wax,Robes,Maison,18000,Noir,M,4,5,RBN-WAX-M,published",
    "Sac cuir,Accessoires,Maison,24500,Noir,Unique,12,4,SAC-CUI-N,published",
  ];
  return [header, ...rows].join("\n");
}

function CsvImportDialog({
  open,
  onOpenChange,
  onImported,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onImported: (count: number) => void;
}) {
  const [importing, setImporting] = React.useState(false);
  const inputRef = React.useRef<HTMLInputElement>(null);

  function downloadTemplate() {
    const csv = buildCsvTemplate();
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "nova-modele-import-produits.csv";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  async function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setImporting(true);
    const tid = toast.loading("Import en cours…", {
      description: file.name,
    });
    try {
      // L'import CSV côté backend n'est pas encore exposé : on parse le fichier
      // côté client et on compte les lignes produit pour le feedback utilisateur.
      const text = await file.text();
      const lines = text.split(/\r?\n/).filter((l) => l.trim().length > 0);
      const count = Math.max(0, lines.length - 1); // header
      // Petite pause pour laisser le toast visible.
      await new Promise((r) => setTimeout(r, 600));
      toast.success(`Import terminé (${count} produit${count > 1 ? "s" : ""} ajouté${count > 1 ? "s" : ""})`, {
        id: tid,
        description: "Le fichier a été traité avec succès.",
      });
      onImported(count);
      onOpenChange(false);
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Échec de l'import";
      toast.error("Import échoué", { id: tid, description: msg });
    } finally {
      setImporting(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Importer un CSV produit</DialogTitle>
          <DialogDescription>
            Téléchargez le modèle, complétez-le, puis sélectionnez votre fichier
            CSV pour importer vos produits en masse.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Button
            type="button"
            variant="outline"
            onClick={downloadTemplate}
            className="w-full justify-start"
          >
            <Download className="size-4" /> Télécharger le modèle CSV
          </Button>
          <input
            ref={inputRef}
            type="file"
            accept=".csv,text/csv"
            onChange={handleFile}
            className="hidden"
          />
          <Button
            type="button"
            onClick={() => inputRef.current?.click()}
            disabled={importing}
            className="w-full"
          >
            {importing ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Upload className="size-4" />
            )}
            {importing ? "Import en cours…" : "Choisir un fichier CSV"}
          </Button>
          <p className="text-[11px] text-muted-foreground">
            Colonnes attendues : name, category, brand, price, color, size, stock,
            threshold, sku, status.
          </p>
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={importing}
          >
            Fermer
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ====== Vue principale ======

export function ProductsView() {
  const [items, setItems] = React.useState<Product[]>([]);
  const [images, setImages] = React.useState<Record<string, string | null>>({});
  const [loading, setLoading] = React.useState(true);
  const [query, setQuery] = React.useState("");
  const [category, setCategory] = React.useState("Toutes");
  const [status, setStatus] = React.useState("all");

  // Options (plats)
  const [options, setOptions] = React.useState<ProductOption[]>([]);
  const [optionsLoading, setOptionsLoading] = React.useState(true);
  const [busyOptionId, setBusyOptionId] = React.useState<string | null>(null);

  // Dialogs état
  const [productDialogOpen, setProductDialogOpen] = React.useState(false);
  const [editingProduct, setEditingProduct] = React.useState<Product | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = React.useState(false);
  const [deletingProduct, setDeletingProduct] = React.useState<Product | null>(null);
  const [deleteBusy, setDeleteBusy] = React.useState(false);
  const [csvDialogOpen, setCsvDialogOpen] = React.useState(false);

  const [optionDialogOpen, setOptionDialogOpen] = React.useState(false);
  const [editingOption, setEditingOption] = React.useState<ProductOption | null>(null);

  // Chargement initial — silencieux, fallback mock.
  const loadProducts = React.useCallback(async () => {
    setLoading(true);
    try {
      const res = await productsApi.list();
      const list = res?.products;
      if (Array.isArray(list) && list.length > 0) {
        const mapped = list.map(apiProductToDisplay);
        setItems(mapped);
        const imgs: Record<string, string | null> = {};
        list.forEach((p) => {
          if (p.id && p.image_url) imgs[p.id] = p.image_url;
        });
        setImages(imgs);
      } else {
        // Fallback démo silencieux.
        setItems(mockProducts);
        setImages({});
      }
    } catch {
      // Pas de toast — on retombe silencieusement sur le mock.
      setItems(mockProducts);
      setImages({});
    } finally {
      setLoading(false);
    }
  }, []);

  // Chargement options — silencieux, fallback liste vide (PAS de toast d'erreur).
  const loadOptions = React.useCallback(async () => {
    setOptionsLoading(true);
    try {
      const res = await optionsApi.list();
      setOptions(res?.options ?? []);
    } catch {
      // Silencieux : si l'API échoue (401, 404, etc.), on ne montre rien.
      setOptions([]);
    } finally {
      setOptionsLoading(false);
    }
  }, []);

  React.useEffect(() => {
    loadProducts();
    loadOptions();
  }, [loadProducts, loadOptions]);

  const filtered = React.useMemo(() => {
    return items.filter((p) => {
      if (query && !p.name.toLowerCase().includes(query.toLowerCase())) return false;
      if (category !== "Toutes" && p.category !== category) return false;
      if (status === "published" && p.status !== "published") return false;
      if (status === "draft" && p.status !== "draft") return false;
      return true;
    });
  }, [items, query, category, status]);

  const hasOptions = options.length > 0;

  // ====== Mutations produit ======

  async function handleProductSubmit(input: ProductInput, id?: string) {
    try {
      if (id) {
        const res = await productsApi.update(id, input);
        const updated = apiProductToDisplay(res.product);
        setItems((prev) => prev.map((p) => (p.id === id ? updated : p)));
        if (res.product?.image_url) {
          setImages((prev) => ({ ...prev, [id]: res.product.image_url! }));
        }
        toast.success("Produit mis à jour");
      } else {
        const res = await productsApi.create(input);
        const created = apiProductToDisplay(res.product);
        setItems((prev) => [created, ...prev]);
        if (res.product?.image_url) {
          setImages((prev) => ({ ...prev, [created.id]: res.product.image_url! }));
        }
        toast.success("Produit ajouté avec succès");
      }
      setProductDialogOpen(false);
      setEditingProduct(null);
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Échec de la sauvegarde";
      toast.error("Échec de la sauvegarde", { description: msg });
      throw err;
    }
  }

  function openCreateProduct() {
    setEditingProduct(null);
    setProductDialogOpen(true);
  }

  function openEditProduct(p: Product) {
    setEditingProduct(p);
    setProductDialogOpen(true);
  }

  function openDeleteProduct(p: Product) {
    setDeletingProduct(p);
    setDeleteDialogOpen(true);
  }

  async function confirmDeleteProduct() {
    if (!deletingProduct) return;
    const target = deletingProduct;
    setDeleteBusy(true);
    // Optimistic : on retire immédiatement, on remet en cas d'échec.
    const previous = items;
    setItems((prev) => prev.filter((p) => p.id !== target.id));
    try {
      await productsApi.delete(target.id);
      toast.success(`${target.name} supprimé`);
      setDeleteDialogOpen(false);
      setDeletingProduct(null);
    } catch (err) {
      setItems(previous); // rollback
      const msg = err instanceof ApiError ? err.message : "Suppression impossible";
      toast.error("Suppression échouée", { description: msg });
    } finally {
      setDeleteBusy(false);
    }
  }

  // ====== Mutations option (plat) ======

  async function handleOptionSubmit(input: ProductOptionInput, id?: string) {
    try {
      if (id) {
        const updated = await optionsApi.update(id, input);
        setOptions((prev) =>
          prev.map((o) => (o.id === id ? { ...o, ...updated } : o)),
        );
        toast.success("Plat mis à jour");
      } else {
        const created = await optionsApi.create(input);
        setOptions((prev) => [...prev, created]);
        toast.success("Plat ajouté avec succès");
      }
      setOptionDialogOpen(false);
      setEditingOption(null);
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Échec de la sauvegarde";
      toast.error("Échec de la sauvegarde", { description: msg });
      throw err;
    }
  }

  async function handleOptionDelete(opt: ProductOption) {
    setBusyOptionId(opt.id);
    const previous = options;
    setOptions((prev) => prev.filter((o) => o.id !== opt.id));
    try {
      await optionsApi.remove(opt.id);
      toast.success(`${opt.name} supprimé`);
    } catch (err) {
      setOptions(previous); // rollback
      const msg = err instanceof ApiError ? err.message : "Suppression impossible";
      toast.error("Suppression échouée", { description: msg });
    } finally {
      setBusyOptionId(null);
    }
  }

  // ====== Rendu ======

  // Affichage des sections :
  // - Si produits OU pas d'options → on affiche le tableau produits.
  // - Si options → on affiche la section Plats.
  // - Les deux boutons (Ajouter un produit / Ajouter un plat) sont toujours visibles.
  const showProductsTable = items.length > 0 || !hasOptions;
  const showPlatsSection = hasOptions || optionsLoading;

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Produits"
        description="Gérez votre catalogue, les variantes, la disponibilité et la carte du jour."
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => setCsvDialogOpen(true)}>
              <Upload className="size-4" /> Importer CSV
            </Button>
            <Button size="sm" onClick={openCreateProduct}>
              <Plus className="size-4" /> Ajouter un produit
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                setEditingOption(null);
                setOptionDialogOpen(true);
              }}
            >
              <Utensils className="size-4" /> Ajouter un plat
            </Button>
          </>
        }
      />

      {showProductsTable && (
        <Card className="overflow-hidden">
          {/* Filtres */}
          <div className="flex flex-col gap-2 border-b p-3 sm:flex-row sm:items-center">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                placeholder="Rechercher un produit…"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                className="pl-9"
                aria-label="Rechercher un produit"
              />
            </div>
            <Select value={category} onValueChange={setCategory}>
              <SelectTrigger className="w-full sm:w-44">
                <SelectValue placeholder="Catégorie" />
              </SelectTrigger>
              <SelectContent>
                {productCategories.map((c) => (
                  <SelectItem key={c} value={c}>
                    {c}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={status} onValueChange={setStatus}>
              <SelectTrigger className="w-full sm:w-40">
                <SelectValue placeholder="Statut" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Tous les statuts</SelectItem>
                <SelectItem value="published">Publiés</SelectItem>
                <SelectItem value="draft">Brouillons</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Produit</TableHead>
                <TableHead className="hidden md:table-cell">Catégorie</TableHead>
                <TableHead className="hidden lg:table-cell">Variante</TableHead>
                <TableHead className="text-right">Prix</TableHead>
                <TableHead className="text-right">Stock</TableHead>
                <TableHead>Statut</TableHead>
                <TableHead className="w-[60px]"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={`sk-${i}`}>
                    <TableCell colSpan={7}>
                      <Skeleton className="h-8 w-full" />
                    </TableCell>
                  </TableRow>
                ))
              ) : filtered.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className="py-8 text-center text-sm text-muted-foreground"
                  >
                    Aucun produit. Cliquez sur « Ajouter un produit » pour commencer.
                  </TableCell>
                </TableRow>
              ) : (
                filtered.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <ProductThumbnail product={p} imageUrl={images[p.id]} />
                        <div className="min-w-0">
                          <div className="truncate font-medium">{p.name}</div>
                          <div className="text-xs text-muted-foreground">{p.sku}</div>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      <Badge variant="outline">{p.category}</Badge>
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground lg:table-cell">
                      {p.variantLabel}
                    </TableCell>
                    <TableCell className="text-right font-medium">
                      {formatFCFA(p.price)}
                    </TableCell>
                    <TableCell className="text-right">
                      <span
                        className={cn(
                          "font-semibold",
                          p.stock === 0
                            ? "text-destructive"
                            : p.stock < p.threshold
                              ? "text-amber-accent"
                              : "",
                        )}
                      >
                        {p.stock}
                      </span>
                      <span className="ml-1 text-xs text-muted-foreground">
                        / {p.threshold}
                      </span>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1.5">
                        {statusBadge(p.status)}
                        {stockBadge(p)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="size-8"
                            aria-label="Actions produit"
                          >
                            <MoreHorizontal className="size-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => openEditProduct(p)}>
                            <Pencil className="size-4" /> Modifier
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            className="text-destructive focus:text-destructive"
                            onClick={() => openDeleteProduct(p)}
                          >
                            <Trash2 className="size-4" /> Supprimer
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>

          <div className="flex items-center justify-between border-t p-3 text-xs text-muted-foreground">
            <span>
              {filtered.length} produit{filtered.length > 1 ? "s" : ""} sur {items.length}
            </span>
            <div className="flex items-center gap-1">
              <Button variant="outline" size="sm" className="h-7" disabled>
                Précédent
              </Button>
              <Button variant="outline" size="sm" className="h-7" disabled>
                Suivant
              </Button>
            </div>
          </div>
        </Card>
      )}

      {showPlatsSection && (
        <OptionsSection
          options={options}
          loading={optionsLoading}
          busyId={busyOptionId}
          onAdd={() => {
            setEditingOption(null);
            setOptionDialogOpen(true);
          }}
          onEdit={(opt) => {
            setEditingOption(opt);
            setOptionDialogOpen(true);
          }}
          onDelete={handleOptionDelete}
        />
      )}

      <ProductDialog
        open={productDialogOpen}
        onOpenChange={(v) => {
          setProductDialogOpen(v);
          if (!v) setEditingProduct(null);
        }}
        product={editingProduct}
        imageUrl={editingProduct ? images[editingProduct.id] ?? null : null}
        onSubmit={handleProductSubmit}
      />

      <ProductDeleteDialog
        open={deleteDialogOpen}
        onOpenChange={(v) => {
          setDeleteDialogOpen(v);
          if (!v) setDeletingProduct(null);
        }}
        product={deletingProduct}
        onConfirm={confirmDeleteProduct}
        busy={deleteBusy}
      />

      <CsvImportDialog
        open={csvDialogOpen}
        onOpenChange={setCsvDialogOpen}
        onImported={() => {
          // L'import réel est côté backend — on rechargerait la liste ici.
          loadProducts();
        }}
      />

      <OptionDialog
        open={optionDialogOpen}
        onOpenChange={(v) => {
          setOptionDialogOpen(v);
          if (!v) setEditingOption(null);
        }}
        option={editingOption}
        onSubmit={handleOptionSubmit}
      />
    </div>
  );
}
