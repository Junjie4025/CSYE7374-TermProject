package folder

import (
	"context"

	csye7374termprojectv1alpha1 "github.com/CSYE7374-TermProject/folder-operator/pkg/apis/example/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	// "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
    "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/aws/credentials"

	"encoding/base64"
)

var log = logf.Log.WithName("controller_folder")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Folder Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFolder{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("folder-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Folder
	err = c.Watch(&source.Kind{Type: &csye7374termprojectv1alpha1.Folder{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Folder
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &csye7374termprojectv1alpha1.Folder{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileFolder implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileFolder{}

// ReconcileFolder reconciles a Folder object
type ReconcileFolder struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Folder object and makes changes based on the state read
// and what is in the Folder.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileFolder) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Folder")

	// Fetch the Folder instance
	instance := &csye7374termprojectv1alpha1.Folder{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("instance is deleted")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	userSecret := &corev1.Secret{}
	getUserSecretErr := r.client.Get(context.TODO(), client.ObjectKey{Namespace: request.Namespace, Name: instance.Spec.UserSecret}, userSecret)
	if getUserSecretErr == nil {
		return reconcile.Result{}, nil
	}

	operatorSecret := &corev1.Secret{}
	getSecretErr := r.client.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: "secret-operator"}, operatorSecret)
	if getSecretErr != nil {
		reqLogger.Info("Cannot find operator secret")
		return reconcile.Result{}, getSecretErr
	}

	bucketName := string(operatorSecret.Data["bucketName"])
	accessKeyId := string(operatorSecret.Data["accessKeyId"])
	accessSecret := string(operatorSecret.Data["accessSecret"])
	reqLogger.Info("Getting bucket name from operator secret: " + bucketName)

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1"),
		Credentials: credentials.NewStaticCredentials(accessKeyId, accessSecret, "")},
	)
	iamService := iam.New(sess);
	
	// create folder in s3 bucket
	folderKey := instance.Spec.Username + "/"
	reqLogger.Info("Create folder in bucket " + bucketName + " and the folder key is " + folderKey)
	s3Service := s3.New(sess)
	_, s3Error := s3Service.PutObject(&s3.PutObjectInput{
		Bucket: &bucketName,
		Key: &folderKey,
	})
	if s3Error != nil {
		reqLogger.Info("Cannot upload folder to s3")
		return reconcile.Result{}, s3Error
	}

	// get user
	getUserInput := &iam.GetUserInput{
		UserName: aws.String(instance.Spec.UserSecret),
	}
	_, getUserErr := iamService.GetUser(getUserInput)

	if getUserErr != nil {
		// create IAM user
		createUserInput := &iam.CreateUserInput{
			UserName: aws.String(instance.Spec.UserSecret),
		}
		iamService.CreateUser(createUserInput)
		reqLogger.Info("create iam user")

		assignUserInput := &iam.AddUserToGroupInput{
			GroupName: aws.String("csye7374"),
			UserName:  aws.String(instance.Spec.UserSecret),
		}
		iamService.AddUserToGroup(assignUserInput)
		reqLogger.Info("assign it to group")

	} else {
		listAccessKeysInput := &iam.ListAccessKeysInput{
			UserName: aws.String(instance.Spec.UserSecret),
		}
		listAccessKeysOutput, _ := iamService.ListAccessKeys(listAccessKeysInput)
		
		for _, accessKeyMetadata := range listAccessKeysOutput.AccessKeyMetadata {
			iamService.DeleteAccessKey(&iam.DeleteAccessKeyInput{AccessKeyId: accessKeyMetadata.AccessKeyId, UserName: aws.String(instance.Spec.UserSecret),})
		}

	}

	// generate access key
	createAccessKeyInput := &iam.CreateAccessKeyInput{
		UserName: aws.String(instance.Spec.UserSecret),
	}
	createAccessKeyResult, _ := iamService.CreateAccessKey(createAccessKeyInput)
	reqLogger.Info("generate access key")

	accessKey := createAccessKeyResult.AccessKey

	newAccessKeyId := accessKey.AccessKeyId
	newSecretAccessKey := accessKey.SecretAccessKey
	
	encodedAccessKeyId := []byte(base64.StdEncoding.EncodeToString([]byte(*newAccessKeyId)))
	encodedAccessSecret := []byte(base64.StdEncoding.EncodeToString([]byte(*newSecretAccessKey)))

	// create new Secret
	secretName := instance.Spec.UserSecret
	reqLogger.Info("new secret name is " + secretName)
	secretNamespace := request.Namespace
	reqLogger.Info("new secret name is " + secretName + " and namespace is " + secretNamespace)

	newSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind: "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
			Namespace: secretNamespace,
		},
		Data: map[string][]byte{
			"accessKeyId": encodedAccessKeyId,
			"accessSecret": encodedAccessSecret,
		},
		Type: "Opaque",
	}

	// Set Folder instance as the owner and controller
	controllerutil.SetControllerReference(instance, newSecret, r.scheme)
	reqLogger.Info("set folder as owner of new secret")

	createSecretErr := r.client.Create(context.TODO(), newSecret)
	if createSecretErr != nil {
		reqLogger.Info("Fail to create a new secret")
		return reconcile.Result{}, createSecretErr
	}

	instance.Status.SetupComplete = true
	r.client.Update(context.TODO(), instance)

	return reconcile.Result{}, nil
}

