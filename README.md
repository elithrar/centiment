# 🤖 centiment

[![GoDoc](https://godoc.org/github.com/elithrar/centiment?status.svg)](https://godoc.org/github.com/elithrar/centiment)
[![CircleCI](https://circleci.com/gh/elithrar/centiment.svg?style=svg)](https://circleci.com/gh/elithrar/centiment)

Centiment is a service that performs sentiment analysis of tweets using Google's [Natural Language APIs](https://cloud.google.com/natural-language/). It was designed with the goal of searching for cryptocurrency tweets, but can be used to analyze and aggregate sentiments for any search terms.

* It will search Twitter for tweets matching the configured search terms, and store the aggregate "sentiment" (negative, neutral or positive) and magnitude each time it runs a search.
* Search terms can be easily added without writing code via `cmd/centimentd/search.toml`
* The aggregate results are made available via a REST API.

The goal is to see whether written sentiment about cryptocurrencies has correlation with prices - e.g. does a negative sentiment predict or otherwise reinforce a drop in price?

## Usage

Centiment relies on Google's [Natural Language APIs](https://cloud.google.com/natural-language/docs/analyzing-sentiment) and [Firestore](https://firebase.google.com/docs/firestore/), but otherwise can run anywhere provided it can reach these services.

At a minimum, you'll need to:

* Install the [Google Cloud SDK](https://cloud.google.com/sdk/) & create a new project with billing enabled.
* Create a new Firestore instance & enable the Natural Language API via the [Google Cloud API Dashboard](https://console.cloud.google.com/apis/dashboard).
* Create a [new Twitter application](https://apps.twitter.com/) & retrieve your API credentials.
* Install the Firebase SDK via `npm install -g firebase-tools`

### Running Locally

You can run Centiment locally with a [properly configured Go toolchain](https://golang.org/doc/install) and [Service Account](https://console.cloud.google.com/apis/credentials) credentials saved locally.

```sh
# Fetch Centiment & its dependencies
go get github.com/elithrar/centiment/...

# Initialize the Firebase SDK & create the required indexes
centiment/ $ firebase login
centiment/ $ firebase deploy --only firestore:indexes

# Set the required configuration as env. variables, or pass via flags (see: `centiment --help`)
export TWITTER_CONSUMER_KEY="key"; \
  export TWITTER_CONSUMER_SECRET="secret"; \
  export TWITTER_ACCESS_TOKEN="at"; \
  export TWITTER_ACCESS_KEY="ak"; \
  export CENTIMENT_PROJECT_ID="your-gcp-project-id"; \
  export GOOGLE_APPLICATION_CREDENTIALS="/path/to/creds.json";

# Run centimentd (the server) in the foreground, provided its on your PATH:
$ centimentd
```

### Deploy to App Engine Flexible

App Engine Flexible makes running Centiment fairly easy: no need to set up or secure an environment.

* `git clone` or `go get` this repository: `git clone https://github.com/elithrar/centiment.git`
* Copy `app.example.yaml` to `app.yaml` and add your Twitter API keys under `env_variables` - important: don't check these credentials into your source-code! The `.gitignore` file included in the repo should help to prevent that.

The service can then be deployed via:

```
centiment $ cd cmd/centimentd
cmd/centimentd $ gcloud app deploy
```

#### Cost

Some notes on running this yourself:

* The default `app.example.yaml` included alongside is designed to use the minimum set of resources on App Engine Flex. Centiment is extremely efficient (it's written in Go) and runs quickly on a single CPU core + 600MB RAM. At the time of writing (Jan 2018), running a 1CPU / 1GB RAM / 10GB disk App Engine Flex instance for a month is ~USD$44/month.
* Cloud Function pricing is fairly cheap for our use-case: if you're running a search every 10 minutes, that's 6 times an hour \* 730 hours per month = 4380 invocations per search term per month. That falls into the [free tier](https://cloud.google.com/functions/pricing) of Cloud Functions pricing.
* The Natural Language API is where the majority of the costs will lie if you choose to run Centiment more aggressively (more tweets, more often). _Searching for up to 50 tweets (per search term) every 10 minutes is 219,000 [Sentiment Analysis records](https://cloud.google.com/natural-language/pricing) per month, and results in a total of USD$219 per search term per month (as of Jan 2018), excluding the small free tier (first 5k)_

> Note: Make sure to do the math before tweaking the `CENTIMENT_RUN_INTERVAL` or `CENTIMENT_MAX_TWEETS` environmental variables, or adding additional search terms to `cmd/centimentd/search.toml`.

### Using BigQuery for Analysis

In order to make analysis easier, you can import data directly into BigQuery after each run via a [Cloud Function](https://firebase.google.com/docs/functions/firestore-events) that is triggered from every database write.

#### Pre-requisites

You'll need to:

* Create a [BigQuery dataset](https://cloud.google.com/bigquery/docs/datasets#create-dataset) called "Centiment" and [a table](https://cloud.google.com/bigquery/docs/tables) called "sentiments". You can opt to use different names, but you will need to make sure to use `config:set` within the Firebase SDK so that our function works.

```sh
# Create an empty table with our schema using the bq CLI tool (installed with the gcloud SDK)
centiment/ $ bq mk --schema bigquery.schema.json -t centiment.sentiments
```

* [Install the Firebase SDK](https://firebase.google.com/docs/functions/get-started) so that we can deploy the Cloud Function with the Firestore trigger.

```sh
centiment $ cd _functions
# Log into your Google Cloud Platform account
_functions $ firebase login
# Set the dataset and table names
_functions $ firebase functions:config:set centiment.dataset="Centiment" centiment.table="sentiments"
# Deploy this secific function.
_functions $ firebase deploy --only functions:sentimentsToBQ
# Done!
```

### Docker

TODO(matt): Create a `Dockerfile` - for this `FROM alpine:latest`

#### Running Elsewhere

If you're running Centiment elsewhere, you'll need to provide the [application with credentials](https://cloud.google.com/docs/authentication/production) to reach Firestore and the Natural Language APIs by setting the `GOOGLE_APPLICATION_CREDENTIALS` environmental variable to the location of your credentials file.

Further, the `Store` interface allows you to provide alternate backend datastores (e.g. PostgreSQL), if you want to run Centiment on alternative infrastructure.

### REST API

Centiment exposes its analysis as JSON via a REST API. Requests are not authenticated by default.

```sh
# Get the latest sentiments for the named currency ("bitcoin", in this case)
GET /sentiments/bitcoin

[
  {
    "id": "lwnXwJmNbxRoE0mzXff0",
    "topic": "bitcoin",
    "slug": "bitcoin",
    "query": "bitcoin OR BTC OR #bitcoin OR #BTC -filter:retweets",
    "count": 154,
    "score": 0.11818181921715863,
    "stdDev": 0.3425117817511681,
    "variance": 0.11731432063835981,
    "fetchedAt": "2018-02-12T05:24:15.44671Z"
  }
]
```

## Contributing

PRs are welcome, but any non-trivial changes should be raised as an issue first to discuss the design and avoid having your hard work rejected!

Suggestions for contributors:

* Additional sentiment analysis adapters (e.g. Azure Cognitive Services, IBM Watson)
* Alternative backend datastores

## License

BSD licensed. See the LICENSE file for details.
