package centiment

import (
	"context"

	"github.com/gosimple/slug"

	"cloud.google.com/go/firestore"
	"github.com/pkg/errors"
	"google.golang.org/api/iterator"
)

// SaveSentiment saves a Sentiment to the datastore, and returns generated ID of
// the new record.
func (fs *Firestore) SaveSentiment(ctx context.Context, sentiment Sentiment) (string, error) {
	var id string

	collection := fs.Store.Collection(fs.CollectionName)
	ref, _, err := collection.Add(ctx, sentiment)
	if err != nil {
		return id, errors.Wrapf(err, "failed to save sentiment (%#v)", sentiment)
	}

	return ref.ID, nil
}

// GetSentimentsByTopic fetches all historical sentiments for the given topic,
// up to limit records. Providing a limit of 0 (or less) will
// fetch all records. Records are ordered from most recent to least recent.
//
// An error (ErrNoResultsFound) will be returned if no records were found.
func (fs *Firestore) GetSentimentsByTopic(ctx context.Context, topic string, limit int) ([]*Sentiment, error) {
	return fs.getSentimentsByField(ctx, "topic", topic, limit)
}

// GetSentimentsBySlug fetches all historical sentiments for the given slug
// ("slugified" topic name) up to limit records. Providing a limit of 0 (or
// less) will fetch all records. Records are ordered from most recent to least
// recent.
//
// An error (ErrNoResultsFound) will be returned if no records were found.
func (fs *Firestore) GetSentimentsBySlug(ctx context.Context, topicSlug string, limit int) ([]*Sentiment, error) {
	if !slug.IsSlug(topicSlug) {
		return nil, errors.Wrapf(ErrInvalidSlug, "%s is not a valid URL slug", topicSlug)
	}
	return fs.getSentimentsByField(ctx, "slug", topicSlug, limit)
}

// getSentimentsByField returns a slice of Sentiments where field == name, ordered by the most recent timestamp up to limit.
//
// An error (ErrNoResultsFound) will be returned if no records were found.
func (fs *Firestore) getSentimentsByField(ctx context.Context, field string, name string, limit int) ([]*Sentiment, error) {
	collection := fs.Store.Collection(fs.CollectionName)
	query := collection.Where(field, "==", name).OrderBy("fetchedAt", firestore.Desc)

	if limit > 0 {
		query = query.Limit(limit)
	}

	iter := query.Documents(ctx)
	sentiments := make([]*Sentiment, 0, limit)

	// Fetch all Sentiments, marshalling them and appending to our
	// result slice.
	for {
		doc, err := iter.Next()

		if err == iterator.Done {
			break
		}

		if err != nil {
			return nil, err
		}

		var result *Sentiment
		if err := doc.DataTo(&result); err != nil {
			return nil, err
		}

		result.ID = doc.Ref.ID
		sentiments = append(sentiments, result)
	}

	if len(sentiments) == 0 {
		return nil, ErrNoResultsFound
	}

	return sentiments, nil
}

// GetSentimentByID fetches an existing Sentiment by its ID. It will return a
// nil value and no error if no record was found.
func (fs *Firestore) GetSentimentByID(ctx context.Context, id string) (*Sentiment, error) {
	collection := fs.Store.Collection(fs.CollectionName)
	iter := collection.Where(firestore.DocumentID, "==", id).Limit(1).Documents(ctx)

	var sentiment *Sentiment

	// Confirm we have the correct document by ID.
	for {
		doc, err := iter.Next()
		if doc.Ref.ID == id {
			if err := doc.DataTo(sentiment); err != nil {
				return nil, err
			}

			break
		}

		if err == iterator.Done {
			break
		}

		if err != nil {
			return nil, err
		}
	}

	return sentiment, nil
}
