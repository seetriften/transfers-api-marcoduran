package repositories

import (
	"context"
	"errors"
	"fmt"
	"transfers-api/internal/config"
	"transfers-api/internal/enums"
	"transfers-api/internal/known_errors"
	"transfers-api/internal/logging"
	"transfers-api/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type TransfersMongoDBRepo struct {
	collection *mongo.Collection
}

type transferMongoDAO struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	SenderID   string             `bson:"sender_id"`
	ReceiverID string             `bson:"receiver_id"`
	Currency   string             `bson:"currency"`
	Amount     float64            `bson:"amount"`
	State      string             `bson:"state"`
}

func NewTransfersMongoDBRepository(cfg config.MongoDB) *TransfersMongoDBRepo {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	uri := fmt.Sprintf("mongodb://%s:%d", cfg.Hostname, cfg.Port)
	if cfg.Username != "" && cfg.Password != "" {
		uri = fmt.Sprintf("mongodb://%s:%s@%s:%d/?authSource=admin", cfg.Username, cfg.Password, cfg.Hostname, cfg.Port)
	}

	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		logging.Logger.Fatalf("error connecting to MongoDB: %v", err)
	}

	collection := client.Database(cfg.Database).Collection(cfg.Collection)
	return &TransfersMongoDBRepo{collection: collection}
}

func (r *TransfersMongoDBRepo) Create(ctx context.Context, transfer models.Transfer) (string, error) {
	dao := transferMongoDAO{
		SenderID:   transfer.SenderID,
		ReceiverID: transfer.ReceiverID,
		Currency:   transfer.Currency.String(),
		Amount:     transfer.Amount,
		State:      transfer.State,
	}

	res, err := r.collection.InsertOne(ctx, dao)
	if err != nil {
		return "", fmt.Errorf("error inserting transfer in MongoDB: %w", err)
	}

	id := res.InsertedID.(primitive.ObjectID).Hex()
	return id, nil
}

func (r *TransfersMongoDBRepo) GetByID(ctx context.Context, id string) (models.Transfer, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return models.Transfer{}, fmt.Errorf("error parsing transfer ID %s: %s: %w", id, err.Error(), known_errors.ErrBadRequest)
	}

	var transfer transferMongoDAO
	if err := r.collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&transfer); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return models.Transfer{}, fmt.Errorf("transfer not found: %w", known_errors.ErrNotFound)
		}
		return models.Transfer{}, fmt.Errorf("error getting transfer: %w", err)
	}

	return models.Transfer{
		ID:         id,
		SenderID:   transfer.SenderID,
		ReceiverID: transfer.ReceiverID,
		Currency:   enums.ParseCurrency(transfer.Currency),
		Amount:     transfer.Amount,
		State:      transfer.State, // TODO: replace with enums.ParseState
	}, nil
}

func (r *TransfersMongoDBRepo) Update(ctx context.Context, transfer models.Transfer) error {
	objID, err := primitive.ObjectIDFromHex(transfer.ID)
	if err != nil {
		return fmt.Errorf("error parsing transfer ID %s: %s: %w", transfer.ID, err.Error(), known_errors.ErrBadRequest)
	}

	update := bson.M{}
	set := bson.M{}

	if transfer.SenderID != "" {
		set["sender_id"] = transfer.SenderID
	}
	if transfer.ReceiverID != "" {
		set["receiver_id"] = transfer.ReceiverID
	}
	if transfer.Currency != enums.CurrencyUnknown {
		set["currency"] = transfer.Currency.String()
	}
	if transfer.Amount != 0 {
		set["amount"] = transfer.Amount
	}
	if transfer.State != "" { // TODO: replace with != enums.StateUnknown
		set["state"] = transfer.State
	}

	if len(set) == 0 {
		return fmt.Errorf("no valid fields to update: %w", known_errors.ErrBadRequest)
	}

	update["$set"] = set

	if _, err := r.collection.UpdateByID(ctx, objID, update); err != nil {
		return fmt.Errorf("error updating transfer: %w", err)
	}
	return nil
}

func (r *TransfersMongoDBRepo) Delete(ctx context.Context, id string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("error parsing transfer ID %s: %s: %w", id, err.Error(), known_errors.ErrBadRequest)
	}

	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": objID})
	if err != nil {
		return fmt.Errorf("error deleting transfer: %w", err)
	}
	if res.DeletedCount == 0 {
		return fmt.Errorf("transfer not found: %w", known_errors.ErrNotFound)
	}
	return nil
}

func (r *TransfersMongoDBRepo) ListByUserID(ctx context.Context, userID string) ([]models.Transfer, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"sender_id": userID},
			{"receiver_id": userID},
		},
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}})
	cur, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("error listing transfers by user: %w", err)
	}
	defer cur.Close(ctx)

	var out []models.Transfer
	for cur.Next(ctx) {
		var dao transferMongoDAO
		if err := cur.Decode(&dao); err != nil {
			return nil, fmt.Errorf("error decoding transfer: %w", err)
		}
		out = append(out, models.Transfer{
			ID:         dao.ID.Hex(),
			SenderID:   dao.SenderID,
			ReceiverID: dao.ReceiverID,
			Currency:   enums.ParseCurrency(dao.Currency),
			Amount:     dao.Amount,
			State:      dao.State,
		})
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transfers: %w", err)
	}
	if out == nil {
		out = []models.Transfer{}
	}
	return out, nil
}
