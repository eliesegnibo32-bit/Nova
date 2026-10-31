"use client";

import * as React from "react";
import { toast } from "sonner";
import { Loader2, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { shopsApi, ApiError } from "@/lib/api";

interface CreateShopDialogProps {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}

const PAYMENT_MODES = [
  { id: "cash", label: "Espèces" },
  { id: "orange_money", label: "Orange Money" },
  { id: "mtn_momo", label: "MTN MoMo" },
  { id: "wave", label: "Wave" },
];

export function CreateShopDialog({ open, onOpenChange }: CreateShopDialogProps) {
  const [submitting, setSubmitting] = React.useState(false);
  const [form, setForm] = React.useState({
    name: "",
    slug: "",
    phone: "+225",
    whatsapp_number: "+225",
    address: "",
    commune: "",
    description: "",
    categories: "",
    accepted_payment_modes: ["cash"] as string[],
  });

  // Auto-generate slug from name
  React.useEffect(() => {
    if (form.name) {
      setForm((prev) => ({
        ...prev,
        slug: prev.name
          .toLowerCase()
          .replace(/[^a-z0-9]+/g, "-")
          .replace(/^-+|-+$/g, ""),
      }));
    }
  }, [form.name]);

  function toggleMode(mode: string) {
    setForm((prev) => ({
      ...prev,
      accepted_payment_modes: prev.accepted_payment_modes.includes(mode)
        ? prev.accepted_payment_modes.filter((m) => m !== mode)
        : [...prev.accepted_payment_modes, mode],
    }));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!form.name || !form.slug || !form.phone) {
      toast.error("Veuillez remplir au moins le nom et le téléphone.");
      return;
    }
    setSubmitting(true);
    try {
      const categories = form.categories
        .split(",")
        .map((c) => c.trim())
        .filter(Boolean);

      const res = await shopsApi.create({
        name: form.name,
        slug: form.slug,
        phone: form.phone,
        whatsapp_number: form.whatsapp_number,
        address: form.address,
        commune: form.commune,
        description: form.description,
        categories,
        accepted_payment_modes: form.accepted_payment_modes,
      } as any);

      toast.success("Boutique créée", {
        description: `${form.name} est prête. Vous pouvez maintenant configurer vos produits.`,
      });
      onOpenChange(false);
      // Reset form
      setForm({
        name: "",
        slug: "",
        phone: "+225",
        whatsapp_number: "+225",
        address: "",
        commune: "",
        description: "",
        categories: "",
        accepted_payment_modes: ["cash"],
      });
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Création impossible";
      toast.error(msg, {
        description: "Note: la création de boutique nécessite un compte admin. Utilisez le Mode démo pour explorer.",
      });
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>Créer une boutique</DialogTitle>
          <DialogDescription>
            Configurez les informations de votre commerce. Vous pourrez modifier ces informations plus tard.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Informations générales */}
          <div className="space-y-3">
            <h4 className="text-sm font-semibold text-foreground">Informations générales</h4>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>Nom de la boutique *</Label>
                <Input
                  required
                  placeholder="Boutique Abidjan Mode"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Slug (auto)</Label>
                <Input
                  placeholder="boutique-abidjan-mode"
                  value={form.slug}
                  onChange={(e) => setForm({ ...form, slug: e.target.value })}
                />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>Téléphone *</Label>
                <Input
                  required
                  placeholder="+225 07 00 00 00 00"
                  value={form.phone}
                  onChange={(e) => setForm({ ...form, phone: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <Label>WhatsApp</Label>
                <Input
                  placeholder="+225 07 00 00 00 00"
                  value={form.whatsapp_number}
                  onChange={(e) => setForm({ ...form, whatsapp_number: e.target.value })}
                />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>Adresse</Label>
                <Input
                  placeholder="Rue des Jardins, Cocody"
                  value={form.address}
                  onChange={(e) => setForm({ ...form, address: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Commune</Label>
                <Input
                  placeholder="Cocody"
                  value={form.commune}
                  onChange={(e) => setForm({ ...form, commune: e.target.value })}
                />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label>Description</Label>
              <Input
                placeholder="Mode et accessoires à Abidjan"
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
              />
            </div>
            <div className="space-y-1.5">
              <Label>Catégories (séparées par des virgules)</Label>
              <Input
                placeholder="Mode, Accessoires, Cosmétique"
                value={form.categories}
                onChange={(e) => setForm({ ...form, categories: e.target.value })}
              />
            </div>
          </div>

          {/* Moyens de paiement */}
          <div className="space-y-2">
            <h4 className="text-sm font-semibold text-foreground">Moyens de paiement acceptés</h4>
            <div className="flex flex-wrap gap-4">
              {PAYMENT_MODES.map((mode) => (
                <div key={mode.id} className="flex items-center gap-2">
                  <Checkbox
                    id={`mode-${mode.id}`}
                    checked={form.accepted_payment_modes.includes(mode.id)}
                    onCheckedChange={() => toggleMode(mode.id)}
                  />
                  <Label htmlFor={`mode-${mode.id}`} className="cursor-pointer text-sm">
                    {mode.label}
                  </Label>
                </div>
              ))}
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={submitting}
            >
              Annuler
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? (
                <>
                  <Loader2 className="size-4 animate-spin" /> Création…
                </>
              ) : (
                <>
                  <Plus className="size-4" /> Créer la boutique
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
