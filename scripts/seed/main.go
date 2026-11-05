// Command seed fills an empty Vrac database with a few weeks of plausible
// work, for demos and manual testing. It refuses non-empty databases.
//
//	go run ./scripts/seed -data data
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yan-imensar/vrac/internal/engine"
)

var ctx = context.Background()

// Entities created on first reference, with their types.
var entities = map[string][]string{
	"Northwind":       {"project"},
	"Northwind/Lot 2": {"project"},
	"Globex":          {"project"},
	"Helios":          {"project"},
	"Umbrella":        {"project"},
	"Homelab":         {"project"},
	"Vrac":            {"project"},
	"Bruno":           {"person"},
	"Alice":           {"person"},
	"Sophie Durand":   {"person"},
	"Marc":            {"person"},
	"Julie":           {"person"},
	"Camille":         {"person"},
	"Thomas":          {"person"},
}

// item is a draft with an optional final status.
type item struct {
	in   string
	done bool
	kids []item
}

func n(in string, kids ...item) item  { return item{in: in, kids: kids} }
func ok(in string, kids ...item) item { return item{in: in, done: true, kids: kids} }

// day is one journal day: offset from today, who wrote, and the content.
type day struct {
	offset int
	actor  engine.Actor
	items  []item
}

var (
	me      = engine.Actor{Name: "owner", Source: "web"}
	mail    = engine.Actor{Name: "Claude", Source: "outlook"}
	teams   = engine.Actor{Name: "Claude", Source: "teams"}
	meeting = engine.Actor{Name: "Claude", Source: "meeting"}
)

var journal = []day{
	{-25, me, []item{
		n("Northwind #project"),
		n("Globex #project"),
		n("Kick-off du projet [[Northwind]] : refonte du portail client, livraison en deux lots"),
		n("#meeting de lancement [[Northwind]] avec [[Bruno]] et [[Marc]] 9h30",
			n("Périmètre : portail client, espace documents, notifications"),
			n("Le lot 1 couvre l'authentification et l'espace documents #decision"),
			ok("Envoyer le planning prévisionnel à [[Bruno]] vendredi #task"),
			ok("Préparer l'estimation du lot 1 #task"),
		),
	}},
	{-24, mail, []item{
		n("[[Marc]] confirme les accès à l'environnement de recette [[Northwind]]"),
		ok("Créer les comptes de test pour la recette [[Northwind]] demain #task"),
	}},
	{-23, me, []item{
		n("Lecture de la doc API [[Globex]] : l'export CSV est limité à 10 000 lignes"),
		n("#idea un mode hors ligne pour l'espace documents [[Northwind]]"),
	}},
	{-22, teams, []item{
		n("#meeting point hebdo [[Globex]] avec [[Alice]] 14h",
			n("Les imports nocturnes échouent depuis lundi"),
			ok("Analyser les logs d'import [[Globex]] #task"),
			n("On ajoute une alerte sur l'échec des imports #decision"),
		),
	}},
	{-21, me, []item{
		ok("Revue de code du module d'authentification [[Northwind]] #task"),
		n("Helios #project"),
		n("[[Helios]] : appel d'offres reçu, réponse attendue sous trois semaines"),
	}},
	{-18, meeting, []item{
		n("#meeting avec [[Sophie Durand]] sur [[Helios]] 11h",
			n("Besoin : outil de suivi des chantiers, 40 utilisateurs terrain"),
			n("Application mobile indispensable, mode hors ligne souhaité"),
			ok("Rédiger la note de cadrage [[Helios]] #task"),
			ok("Envoyer les références clients à [[Sophie Durand]] #task"),
		),
	}},
	{-17, mail, []item{
		n("[[Bruno]] demande une démo intermédiaire du lot 1 [[Northwind]] avant la fin du mois"),
		ok("Planifier la démo intermédiaire [[Northwind]] #task"),
	}},
	{-16, me, []item{
		n("Homelab #project"),
		ok("Migrer les sauvegardes [[Homelab]] vers le nouveau disque #task"),
		n("#idea un tableau de bord unique pour les services du [[Homelab]]"),
	}},
	{-15, teams, []item{
		n("#meeting point hebdo [[Globex]] avec [[Alice]] 14h",
			n("Cause des échecs : changement de format côté fournisseur"),
			n("On passe à l'API v2 plutôt qu'à l'export CSV #decision"),
			ok("Estimer la migration vers l'API v2 [[Globex]] #task"),
		),
	}},
	{-14, me, []item{
		n("Note de cadrage [[Helios]] envoyée, en attente de retour"),
		n("Umbrella #project"),
		n("[[Umbrella]] : nouvelle demande d'évolution sur la recette, à chiffrer"),
	}},
	{-11, meeting, []item{
		n("#meeting démo intermédiaire [[Northwind]] avec [[Bruno]] et [[Julie]] 10h",
			n("Retours positifs sur l'espace documents"),
			n("Le design des notifications est à revoir"),
			n("Le lot 2 portera sur les notifications et le tableau de bord #decision"),
			ok("Partager le compte rendu de démo [[Northwind]] #task"),
			n("Proposer deux maquettes de notifications à [[Julie]] #task"),
		),
	}},
	{-10, mail, []item{
		n("[[Sophie Durand]] valide la note de cadrage [[Helios]], demande un chiffrage détaillé"),
		n("Chiffrer le développement mobile [[Helios]] vendredi #task"),
	}},
	{-9, me, []item{
		n("Ateliers [[Northwind/Lot 2]] à organiser avec les utilisateurs"),
		ok("Envoyer le nouveau zip de recette à [[Umbrella]] #task"),
	}},
	{-8, teams, []item{
		n("#meeting point hebdo [[Globex]] avec [[Alice]] 14h",
			n("Migration API v2 démarrée, premiers appels concluants"),
			n("Envoyer le cahier des charges des imports à [[Alice]] mardi #task"),
		),
	}},
	{-7, me, []item{
		n("Vrac #project"),
		n("#idea [[Vrac]] : un journal où l'IA écrit à ma place après chaque réunion"),
		ok("Écrire la spec V1 de [[Vrac]] #task"),
	}},
	{-4, meeting, []item{
		n("#meeting atelier utilisateurs [[Northwind/Lot 2]] avec [[Julie]] et [[Camille]] 14h",
			n("Les gestionnaires veulent des notifications regroupées par jour"),
			n("Les alertes urgentes restent immédiates #decision"),
			n("Rédiger les user stories notifications [[Northwind/Lot 2]] #task"),
			n("Envoyer la synthèse de l'atelier à [[Bruno]] #task"),
		),
	}},
	{-3, mail, []item{
		n("[[Thomas]] (achats [[Helios]]) demande le chiffrage avant la fin de semaine"),
		n("Relancer [[Sophie Durand]] sur le calendrier de décision [[Helios]] #task"),
	}},
	{-2, me, []item{
		n("[[Globex]] : l'API v2 impose une pagination par curseur, à prévoir dans l'import"),
		n("Mettre à jour le schéma d'import [[Globex]] #task"),
		n("#idea réutiliser le moteur de notifications [[Northwind]] pour [[Helios]]"),
	}},
	{-1, teams, []item{
		n("#meeting point hebdo [[Globex]] avec [[Alice]] 14h",
			n("Bascule sur l'API v2 prévue la semaine prochaine"),
			n("On garde l'export CSV en secours pendant un mois #decision"),
			n("Préparer le plan de bascule API v2 [[Globex]] jeudi #task"),
		),
		n("Passage en revue des tâches [[Northwind]] avec [[Marc]], rien de bloquant"),
	}},
	{0, me, []item{
		n("#meeting avec [[Bruno]] sur [[Northwind]] 16h",
			n("Discussion sur le planning du lot 2"),
		),
		n("Finaliser le chiffrage [[Helios]] demain #task"),
	}},
	{2, mail, []item{
		n("Comité de pilotage [[Northwind]] 10h #meeting"),
	}},
	{6, me, []item{
		n("Déplacement à Lyon pour [[Helios]] #event"),
	}},
	{9, mail, []item{
		n("Bascule API v2 [[Globex]] 9h #event"),
	}},
}

var refRe = regexp.MustCompile(`\[\[([^\]|]+)\]\]`)

func main() {
	data := flag.String("data", "data", "data directory")
	zone := flag.String("timezone", "Europe/Paris", "time zone of journal days")
	flag.Parse()
	if err := run(filepath.Join(*data, "vrac.sqlite"), *zone); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(path, zone string) error {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return err
	}
	e, err := engine.Open(path, engine.Options{Location: loc})
	if err != nil {
		return err
	}
	defer e.Close()
	if days, _ := e.Days(ctx, "", 1); len(days) > 0 {
		return errors.New("the database is not empty; seed only fills a new one")
	}
	if roots, _ := e.Roots(ctx, "", 1); len(roots.Nodes) > 0 {
		return errors.New("the database is not empty; seed only fills a new one")
	}
	if _, err := e.CreateType(ctx, me, "seed-type-idea", "idea"); err != nil {
		return err
	}
	created := map[string]bool{}
	today, _ := time.ParseInLocation("2006-01-02", e.Today(), loc)
	count, req := 0, 0
	for _, d := range journal {
		date := today.AddDate(0, 0, d.offset).Format("2006-01-02")
		for _, it := range d.items {
			drafts, done := draft(it)
			var creations []engine.Creation
			for _, name := range names(it) {
				if !created[name] {
					if types, ok := entities[name]; ok {
						creations = append(creations, engine.Creation{Name: name, Types: types})
					}
				}
			}
			// Entities written as their own line ("Northwind #project") are created
			// by the line itself.
			for name := range entities {
				if strings.HasPrefix(it.in, name+" #") {
					created[name] = true
				}
			}
			req++
			res, err := e.Create(ctx, d.actor, engine.CreateRequest{RequestID: fmt.Sprintf("seed-%d", req), Placement: engine.Placement{Day: date},
				Nodes: []engine.Draft{drafts}, Create: creations, ParseDates: true})
			if err != nil {
				return fmt.Errorf("%s %q: %w", date, it.in, err)
			}
			for _, c := range creations {
				created[c.Name] = true
			}
			for i, nd := range res.Nodes {
				count++
				if done[i] {
					req++
					if _, err := e.Update(ctx, me, engine.UpdateRequest{RequestID: fmt.Sprintf("seed-%d", req), ID: nd.ID, ExpectedRevision: nd.Revision, Status: ptr("done")}); err != nil {
						return err
					}
				}
			}
		}
	}
	fmt.Printf("seeded %d nodes over %d journal days\n", count, len(journal))
	return nil
}

// draft converts an item and returns, in creation order, which nodes are done.
func draft(it item) (engine.Draft, []bool) {
	d := engine.Draft{Input: it.in}
	done := []bool{it.done}
	for _, k := range it.kids {
		kd, kdone := draft(k)
		d.Children = append(d.Children, kd)
		done = append(done, kdone...)
	}
	return d, done
}

func names(it item) []string {
	var out []string
	for _, m := range refRe.FindAllStringSubmatch(it.in, -1) {
		out = append(out, m[1])
	}
	for _, k := range it.kids {
		out = append(out, names(k)...)
	}
	return out
}

func ptr(s string) *string { return &s }
