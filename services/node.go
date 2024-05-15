package services

import (
	"context"
	"sync"

	"github.com/bionicosmos/aegle/edge"
	pb "github.com/bionicosmos/aegle/edge/xray"
	"github.com/bionicosmos/aegle/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func DeleteNode(id *primitive.ObjectID) error {
	ctx := context.Background()
	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(
		ctx,
		func(ctx mongo.SessionContext) (interface{}, error) {
			node, err := models.DeleteNode(ctx, id)
			if err != nil {
				return nil, err
			}
			profiles, err := models.DeleteProfiles(
				ctx,
				bson.M{"name": bson.M{"$in": node.ProfileNames}},
			)
			if err != nil {
				return nil, err
			}
			for _, profile := range profiles {
				if err := models.UpdateUsers(
					ctx,
					bson.M{"_id": bson.M{"$in": profile.UserIds}},
					bson.M{
						"$pull": bson.M{
							"profiles": bson.M{"name": profile.Name},
						},
					},
				); err != nil {
					return nil, err
				}
			}
			if len(node.ProfileNames) > 0 {
				wg := sync.WaitGroup{}
				errCh := make(chan error)
				for _, profileName := range node.ProfileNames {
					wg.Add(1)
					go func() {
						defer wg.Done()
						errCh <- edge.RemoveInbound(
							node.APIAddress,
							&pb.RemoveInboundRequest{Name: profileName},
						)
					}()
				}
				go func() {
					wg.Wait()
					close(errCh)
				}()
				for err := range errCh {
					if err != nil {
						return nil, err
					}
				}
			}
			return nil, nil
		},
	)
	return err
}
